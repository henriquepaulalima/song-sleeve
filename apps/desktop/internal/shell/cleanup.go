package shell

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// Account deletion on this device (§7.11, R-AUTH-6). Every installed-app login
// gets a restricted cleanup credential, kept in the credential store apart from
// the session so it survives logout. A 401 alone never deletes anything; only the
// deletion status read with that credential (or the server saying so on a live
// session) starts a purge. A purge is checkpointed in state.json first, so an
// interrupted one resumes at the next start, and it ends with an ack to the
// server, retried until it gets through.

func cleanupKey(accountID string) string { return "cleanup-" + accountID }

// sweepEvery is how often accounts known on this device are checked for a
// deletion made elsewhere, and unacknowledged purges are retried.
const sweepEvery = 15 * time.Minute

func (s *Shell) saveCleanup(accountID, credential string) {
	if credential == "" {
		return
	}
	if err := s.creds.Set(cleanupKey(accountID), credential); err != nil {
		slog.Warn("save cleanup credential", "err", err)
	}
}

// deleted reports whether the account was confirmed deleted. Without a
// credential, or without an answer, it's false: keep the data.
func (s *Shell) deleted(ctx context.Context, accountID string) bool {
	cred, err := s.creds.Get(cleanupKey(accountID))
	if err != nil || cred == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ok, err := s.cleanup.DeletionStatus(ctx, cred)
	return err == nil && ok
}

// sessionEnded handles a session the server stopped accepting (401): sign out
// and keep the account's data, unless the account was deleted.
func (s *Shell) sessionEnded(accountID string) {
	if s.deleted(context.Background(), accountID) {
		s.purgeAccount(context.Background(), accountID)
		return
	}
	_ = s.dropSession(false)
}

// purgeAccount erases a deleted account from this device: checkpoint, stop and
// delete the namespace, then acknowledge.
func (s *Shell) purgeAccount(ctx context.Context, accountID string) {
	s.mu.Lock()
	if s.st.Purges == nil {
		s.st.Purges = map[string]string{}
	}
	s.st.Purges[accountID] = s.st.Known[accountID]
	_ = s.saveState()
	s.mu.Unlock()
	if a := s.current(); a != nil && a.id == accountID {
		_ = s.dropSession(true)
	}
	s.finishPurges(ctx)
}

// finishPurges completes every checkpointed purge: the namespace goes (again, if
// a run was interrupted), then the server hears about it.
func (s *Shell) finishPurges(ctx context.Context) {
	s.mu.Lock()
	pending := map[string]string{}
	for id, email := range s.st.Purges {
		pending[id] = email
	}
	s.mu.Unlock()
	for id, email := range pending {
		if a := s.current(); a != nil && a.id == id {
			continue // purgeAccount stops it first
		}
		if err := s.removeNamespace(id, email); err != nil {
			slog.Warn("purge", "account", id, "err", err)
			continue
		}
		if cred, _ := s.creds.Get(cleanupKey(id)); cred != "" {
			actx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := s.cleanup.DeletionAck(actx, cred)
			cancel()
			// not_found: Log out and remove of a live account, or already acked.
			if err != nil && api.Code(err) != "not_found" {
				continue // retried by the next sweep
			}
		}
		_ = s.creds.Delete(cleanupKey(id))
		s.mu.Lock()
		delete(s.st.Purges, id)
		delete(s.st.Known, id)
		if s.st.AccountID == id {
			s.st.AccountID = ""
		}
		_ = s.saveState()
		s.mu.Unlock()
		s.emit("account", nil)
	}
}

// removeNamespace deletes an account's data that no running service holds: its
// folder in Song Sleeve, its database and caches, and its session. Source files
// outside Song Sleeve are never touched (R-LOCAL-4).
func (s *Shell) removeNamespace(accountID, email string) error {
	if email != "" {
		if err := os.RemoveAll(filepath.Join(s.cfg.SleeveRoot, songsdir.AccountFolderName(email))); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(s.accountDir(accountID)); err != nil {
		return err
	}
	return s.creds.Delete(accountID)
}

// sweepDeletions checks every account known on this device, signed in or not,
// for a deletion made elsewhere (offline devices are cleaned when they next
// connect, §7.11), and retries unfinished purges.
func (s *Shell) sweepDeletions(ctx context.Context) {
	s.finishPurges(ctx)
	s.mu.Lock()
	var ids []string
	for id := range s.st.Known {
		if _, purging := s.st.Purges[id]; !purging {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if s.deleted(ctx, id) {
			s.purgeAccount(ctx, id)
		}
	}
}

// runSweeps sweeps now and then every sweepEvery until ctx ends.
func (s *Shell) runSweeps(ctx context.Context) {
	for {
		s.sweepDeletions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(sweepEvery):
		}
	}
}
