// Package media holds the song limits (R-LIM) and the worker-side verification of
// uploaded bytes with a sandboxed ffprobe (R-CLOUD-5, F-12).
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

const (
	MaxDurationSeconds = 1800
	MaxDurationMs      = MaxDurationSeconds * 1000
	MaxFileBytes       = 134217728 // 128 MiB, inclusive (R-LIM-1)
)

// Formats is the closed list of supported formats (D-8).
var Formats = map[string]bool{"mp3": true, "aac": true, "flac": true, "wav": true}

type Policy struct {
	MaxDurationSeconds int      `json:"max_duration_seconds"`
	MaxFileBytes       int64    `json:"max_file_bytes"`
	Formats            []string `json:"formats"`
}

func CurrentPolicy() Policy {
	return Policy{MaxDurationSeconds: MaxDurationSeconds, MaxFileBytes: MaxFileBytes, Formats: []string{"mp3", "aac", "flac", "wav"}}
}

// CheckDeclared validates the facts a client declares before a track or grant is
// created (R-LIM-2, second layer).
func CheckDeclared(durationMs int64, size int64, format, sha256 string) error {
	if !Formats[format] {
		return apperr.New(apperr.UnsupportedFormat, "Unsupported format")
	}
	if durationMs <= 0 {
		return apperr.New(apperr.InvalidAudioMetadata, "Unknown or invalid duration")
	}
	if size <= 0 {
		return apperr.New(apperr.InvalidAudioMetadata, "Unknown or invalid size")
	}
	tooLong, tooLarge := durationMs > MaxDurationMs, size > MaxFileBytes
	switch {
	case tooLong && tooLarge:
		return apperr.WithDetails(apperr.SongTooLong, "Song is too long and too large", map[string]any{"also": "song_too_large"})
	case tooLong:
		return apperr.New(apperr.SongTooLong, "Song is longer than 30 minutes")
	case tooLarge:
		return apperr.New(apperr.SongTooLarge, "Song is larger than 128 MiB")
	}
	if len(sha256) != 64 || strings.Trim(strings.ToLower(sha256), "0123456789abcdef") != "" {
		return apperr.Field("sha256", "invalid")
	}
	return nil
}

// Probe is the verified description of an audio file.
type Probe struct {
	Format     string
	DurationMs int64
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

// Verify runs ffprobe on a local file in a separate process with a timeout, a
// memory limit where prlimit exists, the file protocol only and no shell. Any
// timeout, crash or unexpected output rejects the file as invalid_audio_metadata.
func Verify(ctx context.Context, ffprobePath, path string) (*Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-protocol_whitelist", "file", "-print_format", "json", "-show_format", "-show_streams", "file:" + path}
	name := ffprobePath
	if prlimit, err := exec.LookPath("prlimit"); err == nil {
		args = append([]string{"--as=1073741824", "--", ffprobePath}, args...)
		name = prlimit
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, apperr.New(apperr.InvalidAudioMetadata, "The file could not be read as audio")
	}
	var out ffprobeOutput
	dec := json.NewDecoder(&stdout)
	if err := dec.Decode(&out); err != nil {
		return nil, apperr.New(apperr.InvalidAudioMetadata, "The file could not be read as audio")
	}
	format := ""
	for _, s := range out.Streams {
		if s.CodecType != "audio" || s.Disposition.AttachedPic == 1 {
			continue
		}
		format = codecFormat(s.CodecName, out.Format.FormatName)
		break
	}
	if format == "" {
		return nil, apperr.New(apperr.UnsupportedFormat, "Unsupported format")
	}
	secs, err := strconv.ParseFloat(out.Format.Duration, 64)
	if err != nil || math.IsNaN(secs) || math.IsInf(secs, 0) || secs <= 0 {
		return nil, apperr.New(apperr.InvalidAudioMetadata, "Unknown or invalid duration")
	}
	ms := int64(math.Round(secs * 1000))
	if ms > MaxDurationMs {
		return nil, apperr.New(apperr.SongTooLong, "Song is longer than 30 minutes")
	}
	return &Probe{Format: format, DurationMs: ms}, nil
}

// codecFormat maps the real codec, never the extension, to a supported format (D-8).
// ALAC in .m4a, OGG/Opus, WMA and AIFF are rejected.
func codecFormat(codec, container string) string {
	switch {
	case codec == "mp3":
		return "mp3"
	case codec == "aac":
		return "aac"
	case codec == "flac":
		return "flac"
	case strings.HasPrefix(codec, "pcm_") && strings.Contains(container, "wav"):
		return "wav"
	}
	return ""
}

// DurationsMatch tolerates small differences between the duration a browser or
// tag parser reports and ffprobe's measurement.
func DurationsMatch(declaredMs, probedMs int64) bool {
	diff := declaredMs - probedMs
	if diff < 0 {
		diff = -diff
	}
	return diff <= 2000 || float64(diff) <= 0.02*float64(probedMs)
}
