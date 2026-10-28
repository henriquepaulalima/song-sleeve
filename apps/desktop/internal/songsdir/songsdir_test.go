package songsdir

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type fx struct {
	t   *testing.T
	d   *Dir
	ctx context.Context
}

func setup(t *testing.T) *fx {
	t.Helper()
	root := t.TempDir()
	d, err := Open(filepath.Join(root, "Song Sleeve"), "alex@example.com", "acct-1", filepath.Join(root, "private", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	return &fx{t: t, d: d, ctx: context.Background()}
}

func content(id string) (Expected, []byte) {
	data := []byte("audio bytes of " + id)
	s := sha256.Sum256(data)
	return Expected{ContentID: id, FileName: FileName(id, "mp3"), Size: int64(len(data)), SHA256: hex.EncodeToString(s[:])}, data
}

// install puts content through the real install path.
func (f *fx) install(id string) Expected {
	f.t.Helper()
	e, data := content(id)
	tmp, err := f.d.TempFile()
	if err != nil {
		f.t.Fatal(err)
	}
	tmp.Write(data)
	tmp.Close()
	if err := f.d.Install(f.ctx, e, tmp.Name()); err != nil {
		f.t.Fatal(err)
	}
	return e
}

func (f *fx) exists(name string) bool {
	_, err := os.Stat(f.d.Path(name))
	return err == nil
}

func (f *fx) check(expected []Expected, opt CheckOptions) *Report {
	f.t.Helper()
	r, err := f.d.Check(f.ctx, expected, opt)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestInstallVerifiesBytes(t *testing.T) {
	f := setup(t)
	e := f.install("c1")
	if !f.exists(e.FileName) {
		t.Fatal("not installed")
	}
	bad, _ := content("c2")
	tmp, _ := f.d.TempFile()
	tmp.WriteString("tampered")
	tmp.Close()
	if err := f.d.Install(f.ctx, bad, tmp.Name()); err == nil || f.exists(bad.FileName) {
		t.Fatal("a file with the wrong checksum was installed")
	}
	if m := f.d.readManifest(); m == nil || len(m.Entries) != 1 {
		t.Fatal("manifest not updated by install")
	}
	// Remove is tied to the content ID.
	if err := f.d.Remove("other", e.FileName); err == nil {
		t.Fatal("removed a file under another content ID")
	}
	if err := f.d.Remove("c1", e.FileName); err != nil || f.exists(e.FileName) {
		t.Fatal("remove")
	}
}

func TestCheckRepairsTheFolder(t *testing.T) {
	f := setup(t)
	a, b, c := f.install("a"), f.install("b"), f.install("c")
	d, _ := content("d") // expected, never installed
	// The user deletes a mirror, edits another, renames a third and adds a file.
	os.Remove(f.d.Path(a.FileName))
	os.WriteFile(f.d.Path(b.FileName), []byte("edited by hand, same?"), 0o644)
	os.Rename(f.d.Path(c.FileName), f.d.Path("My favourite.mp3"))
	os.WriteFile(f.d.Path("random.txt"), []byte("x"), 0o644)

	r := f.check([]Expected{a, b, c, d}, CheckOptions{Mode: Quick, CaughtUp: true})
	need := map[string]bool{}
	for _, e := range r.NeedsDownload {
		need[e.ContentID] = true
	}
	if !need["a"] || !need["b"] || !need["d"] || need["c"] {
		t.Fatalf("needs download: %v", need)
	}
	if r.Renamed != 1 || !f.exists(c.FileName) || f.exists("My favourite.mp3") {
		t.Fatal("a renamed mirror wasn't renamed back")
	}
	if r.Restored != 1 || f.exists(b.FileName) {
		t.Fatal("an altered mirror wasn't removed for re-download")
	}
	if r.Removed != 1 || f.exists("random.txt") {
		t.Fatalf("extras removed: %d", r.Removed)
	}
}

func TestNothingIsDeletedBeforeCatchingUp(t *testing.T) {
	f := setup(t)
	a := f.install("a")
	os.WriteFile(f.d.Path("extra.mp3"), []byte("x"), 0o644)
	os.WriteFile(f.d.Path(a.FileName), []byte("corrupt"), 0o644)
	// An empty or partial catalog (fresh install, interrupted snapshot) deletes nothing.
	r := f.check(nil, CheckOptions{Mode: Full, CaughtUp: false})
	if r.Removed != 0 || r.DeferredExtras != 2 || !f.exists("extra.mp3") || !f.exists(a.FileName) {
		t.Fatalf("deleted before catching up: %+v", r)
	}
	r = f.check([]Expected{a}, CheckOptions{Mode: Full, CaughtUp: false})
	if len(r.NeedsDownload) != 1 || !f.exists(a.FileName) {
		t.Fatal("an altered file must be kept (and re-downloaded) until caught up")
	}
	// The first check after catching up removes extras.
	r = f.check([]Expected{a}, CheckOptions{Mode: Full, CaughtUp: true})
	if r.Removed != 1 || f.exists("extra.mp3") {
		t.Fatalf("after catching up: %+v", r)
	}
}

func TestLargeDeletionsAskFirst(t *testing.T) {
	f := setup(t)
	for i := 0; i < 21; i++ {
		os.WriteFile(f.d.Path(fmt.Sprintf("x%02d.mp3", i)), []byte("x"), 0o644)
	}
	r := f.check(nil, CheckOptions{CaughtUp: true})
	if r.Removed != 0 || r.LargeDeletionWait != 21 {
		t.Fatalf("automatic check deleted 21 files: %+v", r)
	}
	asked := 0
	r = f.check(nil, CheckOptions{CaughtUp: true, ConfirmLarge: func(n int) bool { asked = n; return false }})
	if asked != 21 || r.Removed != 0 {
		t.Fatal("Keep for now must keep the files")
	}
	r = f.check(nil, CheckOptions{CaughtUp: true, ConfirmLarge: func(int) bool { return true }})
	if r.Removed != 21 {
		t.Fatalf("Delete removed %d", r.Removed)
	}
	// 20 or fewer are removed without asking.
	for i := 0; i < 20; i++ {
		os.WriteFile(f.d.Path(fmt.Sprintf("y%02d.mp3", i)), []byte("y"), 0o644)
	}
	if r := f.check(nil, CheckOptions{CaughtUp: true}); r.Removed != 20 {
		t.Fatalf("20 extras: %+v", r)
	}
}

func TestQuickModeTrustsTheManifestOnlyToSkipWork(t *testing.T) {
	f := setup(t)
	a, b := f.install("a"), f.install("b")
	f.d.hashes = 0
	f.check([]Expected{a, b}, CheckOptions{Mode: Quick, CaughtUp: true})
	if f.d.hashes != 0 {
		t.Fatalf("quick check hashed %d unchanged files", f.d.hashes)
	}
	f.check([]Expected{a, b}, CheckOptions{Mode: Full, CaughtUp: true})
	if f.d.hashes != 2 {
		t.Fatalf("full check hashed %d files", f.d.hashes)
	}
	// Same size but different bytes, with the manifest claiming it's fine: a quick
	// check trusts names and sizes; the full check catches it.
	data, _ := os.ReadFile(f.d.Path(a.FileName))
	data[0] ^= 0xFF
	os.WriteFile(f.d.Path(a.FileName), data, 0o644)
	r := f.check([]Expected{a, b}, CheckOptions{Mode: Full, CaughtUp: true})
	if len(r.NeedsDownload) != 1 || r.NeedsDownload[0].ContentID != "a" {
		t.Fatalf("full check missed a same-size edit: %+v", r)
	}
}

func TestLostManifestIsRebuiltWithoutWiping(t *testing.T) {
	f := setup(t)
	a, b := f.install("a"), f.install("b")
	// Reinstall that kept the folder: the manifest is gone.
	os.Remove(filepath.Join(f.d.AccountDir, ManifestName))
	r := f.check([]Expected{a, b}, CheckOptions{Mode: Quick, CaughtUp: true})
	if !r.ManifestRebuilt || len(r.NeedsDownload) != 0 || !f.exists(a.FileName) || !f.exists(b.FileName) {
		t.Fatalf("rebuild: %+v", r)
	}
	// A corrupted manifest, or one from another account, is the same as missing.
	os.WriteFile(filepath.Join(f.d.AccountDir, ManifestName), []byte("{not json"), 0o644)
	if r := f.check([]Expected{a, b}, CheckOptions{CaughtUp: true}); !r.ManifestRebuilt || len(r.NeedsDownload) != 0 {
		t.Fatal("corrupted manifest")
	}
	other := &Dir{AccountDir: f.d.AccountDir, AccountID: "someone-else"}
	other.writeManifest(&manifest{Entries: []Entry{{FileName: a.FileName}}})
	if r := f.check([]Expected{a, b}, CheckOptions{CaughtUp: true}); !r.ManifestRebuilt {
		t.Fatal("another account's manifest was trusted")
	}
	// A crash mid-write leaves a .tmp beside a valid manifest; the check ignores it.
	os.WriteFile(filepath.Join(f.d.AccountDir, ManifestName+".tmp"), []byte("half"), 0o644)
	if r := f.check([]Expected{a, b}, CheckOptions{CaughtUp: true}); r.ManifestRebuilt || r.Removed != 0 {
		t.Fatalf("leftover temp manifest: %+v", r)
	}
}

func TestProtectedFiles(t *testing.T) {
	f := setup(t)
	a := f.install("a")
	os.WriteFile(f.d.Path(a.FileName), []byte("being played, altered"), 0o644)
	os.WriteFile(f.d.Path("sync-in-progress.flac"), []byte("copy"), 0o644)
	r := f.check([]Expected{a}, CheckOptions{
		Mode: Full, CaughtUp: true,
		Pinned:     map[string]bool{a.FileName: true},
		InProgress: map[string]bool{"sync-in-progress.flac": true},
	})
	if !f.exists(a.FileName) || !f.exists("sync-in-progress.flac") || r.Removed != 0 || len(r.NeedsDownload) != 0 {
		t.Fatalf("protected files touched: %+v", r)
	}
}

func TestRootAndOtherAccounts(t *testing.T) {
	f := setup(t)
	root := f.d.Root
	os.WriteFile(filepath.Join(root, "stray.mp3"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "Random folder"), 0o755)
	os.WriteFile(filepath.Join(root, "Random folder", "a.mp3"), []byte("x"), 0o644)
	// Another account on this installation, even with its manifest lost.
	other, _ := Open(root, "sam@example.com", "acct-2", filepath.Join(t.TempDir(), "tmp"))
	os.WriteFile(other.Path("their-song.mp3"), []byte("theirs"), 0o644)
	// A folder carrying some account's manifest.
	third, _ := Open(root, "old@example.com", "acct-3", filepath.Join(t.TempDir(), "tmp2"))
	third.writeManifest(&manifest{})
	r := f.check(nil, CheckOptions{CaughtUp: true, KnownAccountFolders: []string{"sam@example.com"}})
	if r.Removed != 2 {
		t.Fatalf("root extras removed: %d", r.Removed)
	}
	if _, err := os.Stat(filepath.Join(root, "stray.mp3")); err == nil {
		t.Fatal("stray root file kept")
	}
	if _, err := os.Stat(other.Path("their-song.mp3")); err != nil {
		t.Fatal("another account's folder was touched")
	}
	if _, err := os.Stat(third.AccountDir); err != nil {
		t.Fatal("a folder with an account manifest was removed")
	}
}

func TestNamesAndPurge(t *testing.T) {
	if FileName("c1", "aac") != "c1.m4a" || FileName("c1", "flac") != "c1.flac" {
		t.Fatal("file names")
	}
	if AccountFolderName("Alex/Smith:@Example.com") != "alex_smith_@example.com" {
		t.Fatalf("folder name: %q", AccountFolderName("Alex/Smith:@Example.com"))
	}
	if _, err := Open(t.TempDir(), "../escape", "a", t.TempDir()); err == nil {
		t.Fatal("an account folder escaping the root was accepted")
	}
	f := setup(t)
	f.install("a")
	if err := f.d.Purge(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.d.AccountDir); err == nil {
		t.Fatal("purge kept the account folder")
	}
	if _, err := os.Stat(f.d.Root); err != nil {
		t.Fatal("purge removed the whole Song Sleeve folder")
	}
}

// The playing song's file deleted by hand: nothing to protect, so it is downloaded
// again right away instead of waiting for playback to end.
func TestMissingPinnedFileIsDownloadedAgain(t *testing.T) {
	f := setup(t)
	a := f.install("a")
	os.Remove(f.d.Path(a.FileName))
	r := f.check([]Expected{a}, CheckOptions{Mode: Quick, CaughtUp: true, Pinned: map[string]bool{a.FileName: true}})
	if len(r.NeedsDownload) != 1 || r.NeedsDownload[0].ContentID != "a" {
		t.Fatalf("the missing playing file isn't downloaded again: %+v", r)
	}
}
