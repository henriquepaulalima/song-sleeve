// Package songsdir manages the user-visible Song Sleeve folder (§7.13, R-DEV-4): one
// subfolder per account holding only synced songs, named {contentId}.{ext} (D-20),
// a manifest that records what the app put there (D-29), atomic installs, and the
// folder check that makes the folder an exact mirror of the server.
//
// It only ever deletes inside the account's own folder and the Song Sleeve root
// (R-DEV-3), and it never trusts the folder or the manifest: a size or checksum in
// the manifest only lets it skip work.
package songsdir

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	ManifestName = ".songsleeve-manifest.json"
	// LargeDeletion is the count above which a check asks before deleting (D-29).
	LargeDeletion = 20
	manifestVer   = 1
)

// Extension maps a content format to its file extension (D-20).
func Extension(format string) string {
	switch format {
	case "aac":
		return "m4a"
	case "mp3", "flac", "wav":
		return format
	}
	return "bin"
}

func FileName(contentID, format string) string { return contentID + "." + Extension(format) }

// Entry is one mirrored file as recorded in the manifest.
type Entry struct {
	ContentID string    `json:"content_id"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	SyncedAt  time.Time `json:"synced_at"`
}

type manifest struct {
	Version   int       `json:"version"`
	AccountID string    `json:"account_id"`
	LastCheck time.Time `json:"last_check"`
	Entries   []Entry   `json:"entries"`
}

// Expected is a file that belongs in the folder: a ready track's current content,
// or this device's own pending sync (§7.13 step 2).
type Expected struct {
	ContentID string
	FileName  string
	Size      int64
	SHA256    string
}

type Dir struct {
	Root       string // the Song Sleeve folder
	AccountDir string // Root/{account folder}
	AccountID  string
	tmpDir     string // app-private staging (never inside Root)

	mu sync.Mutex // file work is serialized (R-DEV-1)
	// hashes counts files hashed, so tests can verify quick mode skips work.
	hashes int
}

// Open creates the account folder if needed. tmpDir must be app-private storage.
func Open(root, accountFolder, accountID, tmpDir string) (*Dir, error) {
	if accountFolder == "" || strings.ContainsAny(accountFolder, `/\`) || accountFolder == "." || accountFolder == ".." {
		return nil, fmt.Errorf("invalid account folder name %q", accountFolder)
	}
	d := &Dir{Root: root, AccountDir: filepath.Join(root, accountFolder), AccountID: accountID, tmpDir: tmpDir}
	if err := os.MkdirAll(d.AccountDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, err
	}
	return d, nil
}

// AccountFolderName turns an account email into a safe, recognizable folder name.
func AccountFolderName(email string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(email) {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r < 32:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Trim(b.String(), ". ")
	if s == "" {
		s = "account"
	}
	return s
}

func (d *Dir) Path(fileName string) string { return filepath.Join(d.AccountDir, fileName) }

// TempFile creates a file in app-private staging for a download or sync copy.
func (d *Dir) TempFile() (*os.File, error) { return os.CreateTemp(d.tmpDir, "incoming-*") }

func (d *Dir) hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	d.hashes++
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// readManifest returns the manifest, or nil when it is missing, unreadable or
// belongs to another account.
func (d *Dir) readManifest() *manifest {
	raw, err := os.ReadFile(filepath.Join(d.AccountDir, ManifestName))
	if err != nil {
		return nil
	}
	var m manifest
	if json.Unmarshal(raw, &m) != nil || m.Version != manifestVer || m.AccountID != d.AccountID {
		return nil
	}
	return &m
}

// writeManifest writes atomically (temp file, then rename), so a crash leaves the
// old or the new manifest, never a half-written one.
func (d *Dir) writeManifest(m *manifest) error {
	m.Version, m.AccountID = manifestVer, d.AccountID
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].FileName < m.Entries[j].FileName })
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(d.AccountDir, ManifestName+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if f, err := os.Open(tmp); err == nil {
		_ = f.Sync()
		f.Close()
	}
	return os.Rename(tmp, filepath.Join(d.AccountDir, ManifestName))
}

func (d *Dir) upsertManifest(e Entry) error {
	m := d.readManifest()
	if m == nil {
		m = &manifest{}
	}
	out := m.Entries[:0]
	for _, x := range m.Entries {
		if x.FileName != e.FileName {
			out = append(out, x)
		}
	}
	m.Entries = append(out, e)
	return d.writeManifest(m)
}

// Install moves a verified temp file into the folder atomically (§9.3: temp file,
// verify, atomic install). The temp file must already hold exactly the expected bytes.
func (d *Dir) Install(ctx context.Context, e Expected, tempPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	sum, n, err := d.hashFile(tempPath)
	if err != nil {
		return err
	}
	if sum != e.SHA256 || n != e.Size {
		os.Remove(tempPath)
		return errors.New("checksum mismatch")
	}
	dst := d.Path(e.FileName)
	if err := os.Rename(tempPath, dst); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return err
		}
		// Staging on another filesystem: copy beside the target, then rename.
		if err := copyThenRename(tempPath, dst); err != nil {
			return err
		}
		os.Remove(tempPath)
	}
	return d.upsertManifest(Entry{ContentID: e.ContentID, FileName: e.FileName, Size: e.Size, SHA256: e.SHA256, SyncedAt: time.Now().UTC()})
}

func copyThenRename(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	partial := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".partial")
	out, err := os.Create(partial)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(partial)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(partial)
		return err
	}
	out.Close()
	return os.Rename(partial, dst)
}

// Remove deletes a mirrored file, tied to its content ID so a delayed cleanup never
// removes newer content (R-DEV-3).
func (d *Dir) Remove(contentID, fileName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !strings.HasPrefix(fileName, contentID+".") || strings.ContainsAny(fileName, `/\`) {
		return fmt.Errorf("file %q is not content %s", fileName, contentID)
	}
	if err := os.Remove(d.Path(fileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m := d.readManifest()
	if m == nil {
		return nil
	}
	out := m.Entries[:0]
	for _, x := range m.Entries {
		if x.FileName != fileName {
			out = append(out, x)
		}
	}
	m.Entries = out
	return d.writeManifest(m)
}

// Purge deletes this account's whole folder (Log out and remove, account deletion).
func (d *Dir) Purge() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return os.RemoveAll(d.AccountDir)
}

type Mode int

const (
	Quick Mode = iota // names and sizes against the manifest; hash only what differs
	Full              // hash every file
)

type CheckOptions struct {
	Mode Mode
	// CaughtUp is true only once this session reached the latest cursor or finished
	// a full snapshot. Before that nothing is deleted (§7.13 deletion safety).
	CaughtUp bool
	// Pinned holds the file names of playing files; they are fixed when released.
	Pinned map[string]bool
	// InProgress holds file names of this device's syncs; they are never removed.
	InProgress map[string]bool
	// KnownAccountFolders are other accounts' folders on this installation; the
	// check leaves them alone.
	KnownAccountFolders []string
	// ConfirmLarge is asked before deleting more than LargeDeletion files. Nil means
	// "don't ask now": the deletion waits.
	ConfirmLarge func(n int) bool
}

type Report struct {
	// NeedsDownload: missing or altered files to download again (§7.9 step 4).
	NeedsDownload []Expected
	Renamed       int // extras with exactly the expected bytes, renamed back
	Removed       int // extra files deleted permanently (D-21)
	Restored      int // altered files removed for re-download
	// DeferredExtras are extras kept because the device hasn't caught up, or a
	// large deletion is waiting for an answer.
	DeferredExtras    int
	LargeDeletionWait int // > 0 when the user must answer the large-deletion prompt
	ManifestRebuilt   bool
}

type found struct {
	path string
	name string
	size int64
	dir  bool
}

// Check compares the folder, the manifest and the expected set, and fixes every
// difference it safely can (§7.13 steps 1–7).
func (d *Dir) Check(ctx context.Context, expected []Expected, opt CheckOptions) (*Report, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rep := &Report{}
	m := d.readManifest()
	mode := opt.Mode
	if m == nil {
		// Missing or damaged: never wipe; rebuild in full mode. Files that match the
		// catalog are adopted with no download (cheap reinstall).
		mode, rep.ManifestRebuilt, m = Full, true, &manifest{}
	}
	known := map[string]Entry{}
	for _, e := range m.Entries {
		known[e.FileName] = e
	}

	// Everything currently in the account folder.
	files := map[string]found{}
	entries, err := os.ReadDir(d.AccountDir)
	if err != nil {
		return nil, err
	}
	for _, de := range entries {
		name := de.Name()
		if name == ManifestName || name == ManifestName+".tmp" {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files[name] = found{path: filepath.Join(d.AccountDir, name), name: name, size: info.Size(), dir: de.IsDir()}
	}

	want := map[string]Expected{}
	for _, e := range expected {
		want[e.FileName] = e
	}
	verified := []Entry{}
	extras := map[string]found{}
	for name, f := range files {
		if _, ok := want[name]; !ok {
			extras[name] = f
		}
	}

	// Lazily hashed extras, for renamed-mirror detection.
	extraSums := map[string]string{}
	sumOf := func(f found) string {
		if s, ok := extraSums[f.name]; ok {
			return s
		}
		s, _, err := d.hashFile(f.path)
		if err != nil {
			s = ""
		}
		extraSums[f.name] = s
		return s
	}

	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e := want[name]
		f, present := files[name]
		if opt.Pinned[name] && present {
			if k, ok := known[name]; ok {
				verified = append(verified, k)
			}
			continue // fixed when its pin is released
		}
		// A pinned file that's gone is downloaded again: same content, nothing to
		// replace under the player.
		if present && !f.dir {
			ok := false
			if k, inManifest := known[name]; mode == Quick && inManifest && k.Size == f.size && k.SHA256 == e.SHA256 && f.size == e.Size {
				ok = true // trusted only to skip work
			} else if f.size == e.Size {
				sum, _, err := d.hashFile(f.path)
				ok = err == nil && sum == e.SHA256
			}
			if ok {
				verified = append(verified, Entry{ContentID: e.ContentID, FileName: name, Size: e.Size, SHA256: e.SHA256, SyncedAt: syncedAt(known, name)})
				continue
			}
			// Altered by hand: restored from the cloud, never accepted as an edit.
			rep.NeedsDownload = append(rep.NeedsDownload, e)
			if opt.CaughtUp && !opt.InProgress[name] {
				if err := os.Remove(f.path); err == nil {
					rep.Restored++
				}
			}
			continue
		}
		// Missing: a file with exactly the expected bytes was renamed; rename it back.
		renamed := false
		for xname, x := range extras {
			if x.dir || x.size != e.Size || opt.Pinned[xname] || opt.InProgress[xname] {
				continue
			}
			if sumOf(x) == e.SHA256 {
				if err := os.Rename(x.path, filepath.Join(d.AccountDir, name)); err == nil {
					delete(extras, xname)
					rep.Renamed++
					verified = append(verified, Entry{ContentID: e.ContentID, FileName: name, Size: e.Size, SHA256: e.SHA256, SyncedAt: time.Now().UTC()})
					renamed = true
				}
				break
			}
		}
		if !renamed {
			rep.NeedsDownload = append(rep.NeedsDownload, e)
		}
	}

	// Extras in the account folder, plus anything in the root that isn't an account folder.
	var victims []found
	for name, x := range extras {
		if opt.Pinned[name] || opt.InProgress[name] {
			continue
		}
		victims = append(victims, x)
	}
	rootEntries, _ := os.ReadDir(d.Root)
	accountFolders := map[string]bool{filepath.Base(d.AccountDir): true}
	for _, a := range opt.KnownAccountFolders {
		accountFolders[a] = true
	}
	for _, de := range rootEntries {
		if accountFolders[de.Name()] {
			continue
		}
		if de.IsDir() {
			// A folder with an account manifest belongs to some account; leave it.
			if _, err := os.Stat(filepath.Join(d.Root, de.Name(), ManifestName)); err == nil {
				continue
			}
		}
		victims = append(victims, found{path: filepath.Join(d.Root, de.Name()), name: de.Name(), dir: de.IsDir()})
	}
	count := 0
	for _, v := range victims {
		count += countFiles(v)
	}
	switch {
	case count == 0:
	case !opt.CaughtUp:
		rep.DeferredExtras = count
	case count > LargeDeletion && (opt.ConfirmLarge == nil || !opt.ConfirmLarge(count)):
		rep.DeferredExtras, rep.LargeDeletionWait = count, count
	default:
		for _, v := range victims {
			n := countFiles(v)
			if err := os.RemoveAll(v.path); err == nil {
				rep.Removed += n
			}
		}
	}

	m.Entries, m.LastCheck = verified, time.Now().UTC()
	if err := d.writeManifest(m); err != nil {
		return rep, err
	}
	return rep, nil
}

func syncedAt(known map[string]Entry, name string) time.Time {
	if k, ok := known[name]; ok && !k.SyncedAt.IsZero() {
		return k.SyncedAt
	}
	return time.Now().UTC()
}

func countFiles(f found) int {
	if !f.dir {
		return 1
	}
	n := 0
	_ = filepath.WalkDir(f.path, func(_ string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() {
			n++
		}
		return nil
	})
	if n == 0 {
		n = 1 // an empty folder still counts as one thing removed
	}
	return n
}

// SyncCopyName names this device's in-progress sync copy, before the content ID
// exists. Such files are passed to Check in InProgress and are never removed.
func SyncCopyName(trackID, format string) string { return trackID + ".syncing." + Extension(format) }

// PlaceSyncCopy installs a verified copy of a local song's source file for upload
// (R-CLOUD-4): temp file, verify, atomic move. The source file is never touched.
func (d *Dir) PlaceSyncCopy(tempPath, name, sha string, size int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	sum, n, err := d.hashFile(tempPath)
	if err != nil {
		return err
	}
	if sum != sha || n != size {
		os.Remove(tempPath)
		return errors.New("checksum mismatch")
	}
	dst := d.Path(name)
	if err := os.Rename(tempPath, dst); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return err
		}
		if err := copyThenRename(tempPath, dst); err != nil {
			return err
		}
		os.Remove(tempPath)
	}
	return nil
}

// AdoptSyncCopy renames a published sync copy to its content name and records it in
// the manifest, so the device plays the Songs mirror from then on.
func (d *Dir) AdoptSyncCopy(copyName string, e Expected) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.Rename(d.Path(copyName), d.Path(e.FileName)); err != nil {
		return err
	}
	return d.upsertManifest(Entry{ContentID: e.ContentID, FileName: e.FileName, Size: e.Size, SHA256: e.SHA256, SyncedAt: time.Now().UTC()})
}

// RemoveSyncCopy deletes a failed or cancelled sync copy.
func (d *Dir) RemoveSyncCopy(copyName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !strings.Contains(copyName, ".syncing.") || strings.ContainsAny(copyName, `/\`) {
		return fmt.Errorf("%q is not a sync copy", copyName)
	}
	if err := os.Remove(d.Path(copyName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
