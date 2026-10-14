package media

import (
	"strings"
	"testing"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

func TestCheckDeclared(t *testing.T) {
	sha := strings.Repeat("a", 64)
	cases := []struct {
		ms, size int64
		format   string
		sha      string
		code     apperr.Code
	}{
		{MaxDurationMs, MaxFileBytes, "mp3", sha, ""},
		{MaxDurationMs + 1, 10, "mp3", sha, apperr.SongTooLong},
		{10, MaxFileBytes + 1, "flac", sha, apperr.SongTooLarge},
		{MaxDurationMs + 1, MaxFileBytes + 1, "wav", sha, apperr.SongTooLong},
		{0, 10, "mp3", sha, apperr.InvalidAudioMetadata},
		{10, 0, "mp3", sha, apperr.InvalidAudioMetadata},
		{10, 10, "ogg", sha, apperr.UnsupportedFormat},
		{10, 10, "aac", "xyz", apperr.InvalidField},
	}
	for _, c := range cases {
		err := CheckDeclared(c.ms, c.size, c.format, c.sha)
		if c.code == "" {
			if err != nil {
				t.Errorf("%+v: %v", c, err)
			}
			continue
		}
		if e, ok := apperr.As(err); !ok || e.Code != c.code {
			t.Errorf("%+v: got %v, want %s", c, err, c.code)
		}
	}
	err := CheckDeclared(MaxDurationMs+1, MaxFileBytes+1, "mp3", sha)
	if e, _ := apperr.As(err); e.Details["also"] != "song_too_large" {
		t.Fatal("both limits exceeded must say so")
	}
}

func TestCodecFormat(t *testing.T) {
	cases := map[[2]string]string{
		{"mp3", "mp3"}:                      "mp3",
		{"aac", "mov,mp4,m4a,3gp,3g2,mj2"}:  "aac",
		{"flac", "flac"}:                    "flac",
		{"pcm_s16le", "wav"}:                "wav",
		{"pcm_s24le", "aiff"}:               "",
		{"alac", "mov,mp4,m4a,3gp,3g2,mj2"}: "",
		{"vorbis", "ogg"}:                   "",
		{"opus", "ogg"}:                     "",
		{"wmav2", "asf"}:                    "",
	}
	for in, want := range cases {
		if got := codecFormat(in[0], in[1]); got != want {
			t.Errorf("%v: %q, want %q", in, got, want)
		}
	}
}

func TestDurationsMatch(t *testing.T) {
	if !DurationsMatch(180_000, 181_500) || !DurationsMatch(1_000_000, 1_019_000) {
		t.Fatal("small reading differences must pass")
	}
	if DurationsMatch(180_000, 200_000) {
		t.Fatal("a different song length must fail")
	}
}
