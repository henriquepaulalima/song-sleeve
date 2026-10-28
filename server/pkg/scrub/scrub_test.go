package scrub

import (
	"encoding/json"
	"os"
	"testing"
)

// The cases are shared with the TypeScript scrubber.
func TestSharedCases(t *testing.T) {
	b, err := os.ReadFile("../../../packages/core/src/crash/scrub-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ In, Out string }
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := Text(c.In); got != c.Out {
			t.Errorf("Text(%q)\n got %q\nwant %q", c.In, got, c.Out)
		}
	}
}
