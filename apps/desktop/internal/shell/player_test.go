package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSavePlayer(t *testing.T) {
	dir := t.TempDir()
	snap := json.RawMessage(`{"v":1,"trackId":"local:a","positionSec":42}`)
	if err := savePlayer(dir, snap); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, playerFile))
	if err != nil || string(b) != string(snap) {
		t.Fatalf("saved %q, %v", b, err)
	}
	if err := savePlayer(dir, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, playerFile)); !os.IsNotExist(err) {
		t.Fatalf("null should remove the file, stat err = %v", err)
	}
	if err := savePlayer(dir, nil); err != nil {
		t.Fatal("removing a missing file is fine:", err)
	}
}
