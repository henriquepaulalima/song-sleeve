package probe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fixture struct {
	name   string
	args   []string
	format string // "" means it must be rejected
	err    error
}

var fixtures = []fixture{
	{"cbr.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=7.5", "-b:a", "128k", "-metadata", "title=Harbor Lights", "-metadata", "artist=Marina Okafor"}, "mp3", nil},
	{"vbr.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=9", "-q:a", "4"}, "mp3", nil},
	{"mono22k.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=5", "-ac", "1", "-ar", "22050", "-b:a", "48k"}, "mp3", nil},
	{"noxing.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-b:a", "160k", "-write_xing", "0"}, "mp3", nil},
	{"aac.m4a", []string{"-f", "lavfi", "-i", "sine=frequency=550:duration=6", "-c:a", "aac", "-b:a", "96k", "-metadata", "title=Night Market"}, "aac", nil},
	{"adts.aac", []string{"-f", "lavfi", "-i", "sine=frequency=550:duration=4", "-c:a", "aac", "-f", "adts"}, "aac", nil},
	{"tone.flac", []string{"-f", "lavfi", "-i", "sine=frequency=660:duration=8", "-c:a", "flac", "-metadata", "ARTIST=The Low Tides"}, "flac", nil},
	{"s16.wav", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=3"}, "wav", nil},
	{"f32.wav", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=2.5", "-c:a", "pcm_f32le"}, "wav", nil},
	{"alac.m4a", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=2", "-c:a", "alac"}, "", ErrUnsupported},
	{"tone.ogg", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=2", "-c:a", "libvorbis"}, "", ErrUnsupported},
	{"tone.aiff", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=2"}, "", ErrUnsupported},
	{"mp2.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=330:duration=2", "-c:a", "mp2", "-f", "mp2"}, "", ErrUnsupported},
}

func ffprobeMs(t *testing.T, path string) int64 {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return int64(f * 1000)
}

func TestFixtures(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			path := filepath.Join(dir, fx.name)
			out, err := exec.Command("ffmpeg", append(append([]string{"-loglevel", "error", "-y"}, fx.args...), path)...).CombinedOutput()
			if err != nil {
				t.Skipf("ffmpeg can't make %s: %s", fx.name, out)
			}
			facts, err := File(path)
			if fx.format == "" {
				if !errors.Is(err, fx.err) {
					t.Fatalf("got %v %+v, want %v", err, facts, fx.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if facts.Format != fx.format {
				t.Fatalf("format %q, want %q", facts.Format, fx.format)
			}
			want := ffprobeMs(t, path)
			diff := facts.DurationMs - want
			if diff < 0 {
				diff = -diff
			}
			// The server tolerates 2 s or 2 %; stay well inside that.
			if diff > 250 {
				t.Fatalf("duration %d ms, ffprobe %d ms", facts.DurationMs, want)
			}
			st, _ := os.Stat(path)
			if facts.Bytes != st.Size() || len(facts.SHA256) != 64 {
				t.Fatalf("bytes %d sha %q", facts.Bytes, facts.SHA256)
			}
		})
	}
}

func TestTags(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	cases := map[string][2]string{"cbr.mp3": {"Harbor Lights", "Marina Okafor"}, "aac.m4a": {"Night Market", ""}, "tone.flac": {"", "The Low Tides"}}
	for _, fx := range fixtures {
		want, ok := cases[fx.name]
		if !ok {
			continue
		}
		path := filepath.Join(dir, fx.name)
		if err := exec.Command("ffmpeg", append(append([]string{"-loglevel", "error", "-y"}, fx.args...), path)...).Run(); err != nil {
			t.Fatal(err)
		}
		facts, err := File(path)
		if err != nil {
			t.Fatal(err)
		}
		if facts.Title != want[0] || facts.Artist != want[1] {
			t.Errorf("%s: title %q artist %q", fx.name, facts.Title, facts.Artist)
		}
	}
}

func TestGarbageIsRejected(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"empty.mp3":     {},
		"text.mp3":      []byte("this is not audio at all, just some text that goes on"),
		"truncated.wav": []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
		"badflac.flac":  []byte("fLaC\x80\x00\x00\x22"),
		"cover.jpg":     {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1},
	} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, data, 0o644)
		if facts, err := File(path); err == nil {
			t.Errorf("%s accepted: %+v", name, facts)
		}
	}
}
