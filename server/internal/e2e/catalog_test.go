//go:build integration

package e2e

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestChangeFeed(t *testing.T) {
	c, acc := newAppAccount(t, uniqueEmail("feed"))
	snap := expect(t, c.do("GET", "/me/catalog/snapshot", nil, ""), 200)
	cursor := int(snap["cursor"].(float64))
	p := expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Feed"}), 201)["playlist"].(map[string]any)
	g := expect(t, c.mutate("POST", "/me/groups", map[string]any{"name": "G"}), 201)["group"].(map[string]any)
	expect(t, c.mutate("POST", "/me/groups/"+g["id"].(string)+"/playlists", map[string]any{"playlist_ids": []string{p["id"].(string)}, "expected_revision": g["revision"]}), 200)

	ch := expect(t, c.do("GET", "/me/catalog/changes?cursor="+itoa(cursor), nil, ""), 200)
	if got := ids(ch["playlists"].([]any), "id"); !equal(got, []string{p["id"].(string)}) {
		t.Fatalf("changed playlists: %v", got)
	}
	next := int(ch["cursor"].(float64))
	if next <= cursor || ch["has_more"] != false {
		t.Fatalf("cursor did not advance: %v", ch)
	}
	// Deleting the playlist: it is reported deleted and its group is updated.
	expect(t, c.mutate("DELETE", "/me/playlists/"+p["id"].(string), nil), 200)
	ch = expect(t, c.do("GET", "/me/catalog/changes?cursor="+itoa(next), nil, ""), 200)
	del := ch["deleted"].(map[string]any)["playlist"].([]any)
	if len(del) != 1 || del[0] != p["id"] {
		t.Fatalf("deleted playlists: %v", ch["deleted"])
	}
	groups := ch["groups"].([]any)
	if len(groups) != 1 || len(groups[0].(map[string]any)["playlists"].([]any)) != 0 {
		t.Fatalf("group not updated: %v", groups)
	}
	// Reading the same page again is harmless (R-EVT-3): same cursor, same events.
	again := expect(t, c.do("GET", "/me/catalog/changes?cursor="+itoa(next), nil, ""), 200)
	if again["cursor"] != ch["cursor"] {
		t.Fatal("re-reading the feed moved the cursor")
	}
	// An expired cursor asks for a full snapshot (R-EVT-4).
	if _, err := deps.Pool.Exec(context.Background(), `UPDATE users SET events_purged_through = $2 WHERE id = $1`, acc["id"], next); err != nil {
		t.Fatal(err)
	}
	if r := c.do("GET", "/me/catalog/changes?cursor="+itoa(cursor), nil, ""); r.code() != "cursor_expired" {
		t.Fatalf("expired cursor: %s", r.Raw)
	}
	expect(t, c.do("GET", "/me/catalog/changes?cursor="+itoa(next), nil, ""), 200)
}

func TestSnapshotAndTrackPaging(t *testing.T) {
	c, acc := newAppAccount(t, uniqueEmail("paging"))
	all := insertTracks(t, acc["id"].(string), 1005)
	first := expect(t, c.do("GET", "/me/catalog/snapshot", nil, ""), 200)
	tracks := ids(first["tracks"].([]any), "id")
	token, _ := first["next_page_token"].(string)
	if len(tracks) != 1000 || token == "" || first["artists"] == nil {
		t.Fatalf("first page: %d tracks, token %q", len(tracks), token)
	}
	second := expect(t, c.do("GET", "/me/catalog/snapshot?page_token="+token, nil, ""), 200)
	tracks = append(tracks, ids(second["tracks"].([]any), "id")...)
	if second["next_page_token"] != nil || second["cursor"] != nil {
		t.Fatalf("second page: %v", second["next_page_token"])
	}
	if !equal(tracks, all) {
		t.Fatal("snapshot pages are not the library in order")
	}
	// The paginated list and search (web lists, F-13).
	page := expect(t, c.do("GET", "/me/tracks", nil, ""), 200)
	if len(page["tracks"].([]any)) != 200 || page["next_cursor"] == nil {
		t.Fatal("track list paging")
	}
	found := expect(t, c.do("GET", "/me/tracks?q=Track%201004", nil, ""), 200)["tracks"].([]any)
	if len(found) != 1 {
		t.Fatalf("search: %d results", len(found))
	}
	if n := len(expect(t, c.do("GET", "/me/tracks?q=%25", nil, ""), 200)["tracks"].([]any)); n != 0 {
		t.Fatalf("a literal %% matched %d tracks", n)
	}
	q := expect(t, c.do("GET", "/me/queue-ids?source=library", nil, ""), 200)
	if len(q["track_ids"].([]any)) != 1005 {
		t.Fatal("queue IDs must cover the whole list in one response")
	}
}

func TestOrdering(t *testing.T) {
	c, acc := newAppAccount(t, uniqueEmail("order"))
	tr := insertTracks(t, acc["id"].(string), 4) // a b c d
	rev := func(id string) any {
		for _, x := range expect(t, c.do("GET", "/me/tracks", nil, ""), 200)["tracks"].([]any) {
			if m := x.(map[string]any); m["id"] == id {
				return m["revision"]
			}
		}
		return nil
	}
	order := func() []string {
		return ids2(expect(t, c.do("GET", "/me/queue-ids?source=library", nil, ""), 200)["track_ids"].([]any))
	}
	move := func(item string, after, before any) resp {
		return c.mutate("PUT", "/me/library/songs/order", map[string]any{"item_id": item, "after_id": after, "before_id": before, "expected_revision": rev(item)})
	}
	expect(t, move(tr[3], nil, tr[0]), 200)   // d to top
	expect(t, move(tr[0], tr[1], tr[2]), 200) // a between b and c
	expect(t, move(tr[1], nil, nil), 200)     // b to bottom
	if got, want := order(), []string{tr[3], tr[0], tr[2], tr[1]}; !equal(got, want) {
		t.Fatalf("library order %v, want %v", got, want)
	}
	if r := c.mutate("PUT", "/me/library/songs/order", map[string]any{"item_id": tr[2], "after_id": nil, "before_id": tr[3], "expected_revision": 0}); r.code() != "revision_conflict" {
		t.Fatalf("stale move: %s", r.Raw)
	}

	// Playlist order is independent of library order (R-ORG-6).
	p := expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Order"}), 201)["playlist"].(map[string]any)
	p = expect(t, c.mutate("POST", "/me/playlists/"+p["id"].(string)+"/songs", map[string]any{"track_ids": []string{tr[0], tr[1], tr[2], tr[0]}, "expected_revision": p["revision"]}), 200)["playlist"].(map[string]any)
	if got := ids(p["items"].([]any), "track_id"); !equal(got, []string{tr[0], tr[1], tr[2]}) {
		t.Fatalf("playlist add (duplicates skipped): %v", got)
	}
	p = expect(t, c.mutate("PUT", "/me/playlists/"+p["id"].(string)+"/songs/order", map[string]any{"item_id": tr[2], "after_id": nil, "before_id": tr[0], "expected_revision": p["revision"]}), 200)["playlist"].(map[string]any)
	if got := ids(p["items"].([]any), "track_id"); !equal(got, []string{tr[2], tr[0], tr[1]}) {
		t.Fatalf("playlist order: %v", got)
	}
	q := ids2(expect(t, c.do("GET", "/me/queue-ids?source=playlist:"+p["id"].(string), nil, ""), 200)["track_ids"].([]any))
	if !equal(q, []string{tr[2], tr[0], tr[1]}) {
		t.Fatalf("playlist queue: %v", q)
	}
	p = expect(t, c.mutate("DELETE", "/me/playlists/"+p["id"].(string)+"/songs", map[string]any{"track_ids": []string{tr[0]}, "expected_revision": p["revision"]}), 200)["playlist"].(map[string]any)
	if len(p["items"].([]any)) != 2 || len(order()) != 4 {
		t.Fatal("removing from a playlist must not delete the song (R-ORG-5)")
	}

	// Group order.
	var pls []string
	for _, n := range []string{"One", "Two", "Three"} {
		pls = append(pls, expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": n}), 201)["playlist"].(map[string]any)["id"].(string))
	}
	g := expect(t, c.mutate("POST", "/me/groups", map[string]any{"name": "Grp"}), 201)["group"].(map[string]any)
	g = expect(t, c.mutate("POST", "/me/groups/"+g["id"].(string)+"/playlists", map[string]any{"playlist_ids": pls, "expected_revision": g["revision"]}), 200)["group"].(map[string]any)
	g = expect(t, c.mutate("PUT", "/me/groups/"+g["id"].(string)+"/playlists/order", map[string]any{"item_id": pls[0], "after_id": pls[2], "before_id": nil, "expected_revision": g["revision"]}), 200)["group"].(map[string]any)
	if got := ids(g["playlists"].([]any), "playlist_id"); !equal(got, []string{pls[1], pls[2], pls[0]}) {
		t.Fatalf("group order: %v", got)
	}
	g = expect(t, c.mutate("DELETE", "/me/groups/"+g["id"].(string)+"/playlists", map[string]any{"playlist_ids": []string{pls[1]}, "expected_revision": g["revision"]}), 200)["group"].(map[string]any)
	expect(t, c.mutate("DELETE", "/me/groups/"+g["id"].(string), nil), 200)
	if n := len(expect(t, c.do("GET", "/me/playlists", nil, ""), 200)["playlists"].([]any)); n != 4 {
		t.Fatalf("deleting a group must keep its playlists: %d left", n)
	}
}

func ids2(list []any) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.(string)
	}
	return out
}

func TestArtists(t *testing.T) {
	c, acc := newAppAccount(t, uniqueEmail("artists"))
	tr := insertTracks(t, acc["id"].(string), 2)
	revOf := func(id string) any {
		for _, x := range expect(t, c.do("GET", "/me/tracks", nil, ""), 200)["tracks"].([]any) {
			if m := x.(map[string]any); m["id"] == id {
				return m["revision"]
			}
		}
		return nil
	}
	a := expect(t, c.mutate("PATCH", "/me/tracks/"+tr[0], map[string]any{"expected_revision": revOf(tr[0]), "artist": map[string]any{"name": "The Low Tides"}}), 200)["track"].(map[string]any)
	// The same normalized name reuses the artist (R-ORG-2).
	b := expect(t, c.mutate("PATCH", "/me/tracks/"+tr[1], map[string]any{"expected_revision": revOf(tr[1]), "artist": map[string]any{"name": "  the low   TIDES "}}), 200)["track"].(map[string]any)
	if a["artist_id"] == nil || a["artist_id"] != b["artist_id"] {
		t.Fatalf("artist not reused: %v vs %v", a["artist_id"], b["artist_id"])
	}
	artistID := a["artist_id"].(string)
	// A similar but different name is a new artist: suggestions are client-side only.
	other := expect(t, c.mutate("PATCH", "/me/tracks/"+tr[1], map[string]any{"expected_revision": revOf(tr[1]), "artist": map[string]any{"name": "Low Tides"}}), 200)["track"].(map[string]any)
	if other["artist_id"] == artistID {
		t.Fatal("a similar name was merged automatically")
	}
	list := expect(t, c.do("GET", "/me/artists", nil, ""), 200)["artists"].([]any)
	if len(list) != 2 {
		t.Fatalf("artists: %d", len(list))
	}
	songs := expect(t, c.do("GET", "/me/artists/"+artistID+"/songs", nil, ""), 200)["tracks"].([]any)
	if len(songs) != 1 {
		t.Fatalf("artist songs: %d", len(songs))
	}
	var artistRev any
	for _, x := range list {
		if m := x.(map[string]any); m["id"] == artistID {
			artistRev = m["revision"]
		}
	}
	if r := c.mutate("PATCH", "/me/artists/"+artistID, map[string]any{"name": "low tides", "expected_revision": artistRev}); r.code() != "invalid_field" {
		t.Fatalf("rename onto an existing artist: %s", r.Raw)
	}
	ren := expect(t, c.mutate("PATCH", "/me/artists/"+artistID, map[string]any{"name": "The Low Tides (Band)", "expected_revision": artistRev}), 200)["artist"].(map[string]any)
	if ren["name"] != "The Low Tides (Band)" {
		t.Fatal("rename")
	}
	if r := c.mutate("DELETE", "/me/artists/"+artistID, nil); r.code() != "artist_in_use" {
		t.Fatalf("delete an artist in use: %s", r.Raw)
	}
	expect(t, c.mutate("PATCH", "/me/tracks/"+tr[0], map[string]any{"expected_revision": revOf(tr[0]), "artist": nil}), 200)
	expect(t, c.mutate("DELETE", "/me/artists/"+artistID, nil), 200)
	// A track edit referencing a deleted artist by ID only is refused.
	if r := c.mutate("PATCH", "/me/tracks/"+tr[0], map[string]any{"expected_revision": revOf(tr[0]), "artist": map[string]any{"id": artistID}}); r.code() != "not_found" {
		t.Fatalf("stale artist ID: %s", r.Raw)
	}
}

func TestSyncScopes(t *testing.T) {
	app, _ := newAppAccount(t, uniqueEmail("scopes"))
	if r := app.mutate("PUT", "/me/sync-scopes/library", nil); r.code() != "cloud_sync_unavailable" {
		t.Fatalf("scope without cloud sync: %s", r.Raw)
	}
	c := newWebSyncAccount(t, 1)
	p := expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Scoped"}), 201)["playlist"].(map[string]any)
	expect(t, c.mutate("PUT", "/me/sync-scopes/library", nil), 200)
	expect(t, c.mutate("PUT", "/me/sync-scopes/playlist/"+p["id"].(string), nil), 200)
	s := expect(t, c.mutate("POST", "/me/sync-scopes/library/pause", nil), 200)["scopes"].([]any)
	states := map[string]string{}
	for _, x := range s {
		m := x.(map[string]any)
		states[m["type"].(string)] = m["state"].(string)
	}
	if states["library"] != "paused" || states["playlist"] != "enabled" {
		t.Fatalf("scope states: %v", states)
	}
	if r := c.mutate("PUT", "/me/sync-scopes/group/"+p["id"].(string), nil); r.code() != "not_found" {
		t.Fatalf("scope for a missing group: %s", r.Raw)
	}
	// Deleting the playlist removes its scope.
	expect(t, c.mutate("DELETE", "/me/playlists/"+p["id"].(string), nil), 200)
	if n := len(expect(t, c.do("GET", "/me/sync-scopes", nil, ""), 200)["scopes"].([]any)); n != 1 {
		t.Fatalf("scopes after deleting the playlist: %d", n)
	}
}

func TestEventStream(t *testing.T) {
	c, _ := newAppAccount(t, uniqueEmail("sse"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := sseEvents(t, c, ctx)
	nextEvent(t, events, "ready", 5*time.Second)
	expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Live"}), 201)
	nextEvent(t, events, "changes", 5*time.Second)
	// Another account's changes never reach this stream.
	other, _ := newAppAccount(t, uniqueEmail("sse-other"))
	expect(t, other.mutate("POST", "/me/playlists", map[string]any{"name": "Not yours"}), 201)
	select {
	case e := <-events:
		t.Fatalf("received another account's %q event", e)
	case <-time.After(1500 * time.Millisecond):
	}
}

func TestReplacementConflictBecomesSuggestion(t *testing.T) {
	requireFixtures(t, "mp3", "flac")
	c := newWebSyncAccount(t, 2)
	other := loginApp(t, syncEmails[2], testPassword)
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "published", 20*time.Second, func() bool {
		tr := track(c, u.trackID)
		return tr != nil && tr["state"] == "ready" && tr["operation"] == nil
	})
	cur := track(c, u.trackID)
	data, _ := os.ReadFile(fixtures["flac"])
	op := expect(t, c.mutate("POST", "/me/tracks/"+u.trackID+"/replacement", map[string]any{
		"expected_revision": cur["revision"], "name": "My Choice", "artist": map[string]any{"name": "Chosen Artist"},
		"duration_ms": probeMs(t, fixtures["flac"]), "byte_size": len(data), "format": "flac", "sha256": sha(fixtures["flac"]),
	}), 202)["operation"].(map[string]any)
	// Another device renames the song while the new audio uploads.
	expect(t, other.mutate("PATCH", "/me/tracks/"+u.trackID, map[string]any{"expected_revision": cur["revision"], "name": "Their Edit"}), 200)
	sendParts(t, c, op["id"].(string), op, data)
	waitFor(t, "replacement published", 20*time.Second, func() bool { tr := track(c, u.trackID); return tr["format"] == "flac" && tr["operation"] == nil })
	tr := track(c, u.trackID)
	// The content switch commits; the newer name stays and the user's choice comes
	// back as a suggestion. The untouched artist field applies (R-MUT-5).
	if tr["name"] != "Their Edit" {
		t.Fatalf("a concurrent edit was overwritten: %v", tr["name"])
	}
	sug, _ := tr["metadata_suggestion"].(map[string]any)
	if sug == nil || sug["name"] != "My Choice" {
		t.Fatalf("suggestion: %v", tr["metadata_suggestion"])
	}
	if tr["artist_id"] == nil {
		t.Fatal("the non-conflicting artist choice was not applied")
	}
	resolved := expect(t, c.mutate("PATCH", "/me/tracks/"+u.trackID, map[string]any{"expected_revision": tr["revision"], "name": "My Choice", "resolve_suggestion": true}), 200)["track"].(map[string]any)
	if resolved["metadata_suggestion"] != nil || resolved["name"] != "My Choice" {
		t.Fatalf("resolving the suggestion: %v", resolved)
	}
}

func TestQueuedDeleteRunsAfterReplacement(t *testing.T) {
	requireFixtures(t, "mp3", "flac")
	c := newWebSyncAccount(t, 3)
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "published", 20*time.Second, func() bool {
		tr := track(c, u.trackID)
		return tr != nil && tr["state"] == "ready" && tr["operation"] == nil
	})
	data, _ := os.ReadFile(fixtures["flac"])
	op := expect(t, c.mutate("POST", "/me/tracks/"+u.trackID+"/replacement", map[string]any{
		"expected_revision": track(c, u.trackID)["revision"], "name": "Replaced", "artist": nil,
		"duration_ms": probeMs(t, fixtures["flac"]), "byte_size": len(data), "format": "flac", "sha256": sha(fixtures["flac"]),
	}), 202)["operation"].(map[string]any)
	// The delete waits behind the running replacement (R-SYNC-2).
	del := expect(t, c.mutate("DELETE", "/me/tracks/"+u.trackID, nil), http.StatusAccepted)
	if del["deleted"] != false || del["operation"].(map[string]any)["state"] != "waiting" {
		t.Fatalf("queued delete: %v", del)
	}
	if tr := track(c, u.trackID); tr["operation"].(map[string]any)["kind"] != "replace" {
		t.Fatal("the active replacement should be the one shown")
	}
	sendParts(t, c, op["id"].(string), op, data)
	waitFor(t, "queued delete ran", 30*time.Second, func() bool { return track(c, u.trackID) == nil })
	summ := expect(t, c.do("GET", "/me/summary", nil, ""), 200)
	if summ["storage_used_bytes"].(float64) != 0 || summ["storage_reserved_bytes"].(float64) != 0 {
		t.Fatalf("storage after the delete: %v", summ)
	}
	// The staging object is cleaned up after publication.
	var staging string
	if err := deps.Pool.QueryRow(context.Background(), `SELECT staging_key FROM track_sync_operations WHERE id = $1`, op["id"]).Scan(&staging); err == nil {
		waitFor(t, "staging cleanup", 15*time.Second, func() bool {
			_, err := deps.Store.Head(context.Background(), staging)
			return err != nil
		})
	}
}

func TestUploadGrantsBelongToTheUploadingDevice(t *testing.T) {
	requireFixtures(t, "mp3")
	c := signIn(t, owner)
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	// A second session of the same account is another device.
	other := signInFresh(t, owner)
	if r := other.do("POST", "/me/operations/"+u.opID+"/upload-grant", map[string]any{"part_numbers": []int{1}}, ""); r.code() != "forbidden" {
		t.Fatalf("grant to another device: %s", r.Raw)
	}
	if r := c.do("POST", "/me/operations/"+u.opID+"/upload-grant", map[string]any{"part_numbers": []int{99}}, ""); r.code() != "invalid_field" {
		t.Fatalf("out-of-range part: %s", r.Raw)
	}
	// But any device may cancel it, which fails the sync everywhere.
	expect(t, other.mutate("POST", "/me/operations/"+u.opID+"/cancel", nil), 200)
	if track(c, u.trackID) != nil {
		t.Fatal("a cancelled sync is still listed")
	}
	if r := c.do("POST", "/me/operations/"+u.opID+"/heartbeat", map[string]any{"confirmed_bytes": 10}, ""); r.Status != http.StatusNotFound {
		t.Fatalf("heartbeat after cancel: %d", r.Status)
	}
}

func TestQuotaReservation(t *testing.T) {
	c := signIn(t, owner)
	uid := userID(t, c)
	ctx := context.Background()
	var quota int64
	_ = deps.Pool.QueryRow(ctx, `SELECT storage_quota_bytes FROM users WHERE id = $1`, uid).Scan(&quota)
	var used int64
	_ = deps.Pool.QueryRow(ctx, `SELECT storage_used_bytes + storage_reserved_bytes FROM users WHERE id = $1`, uid).Scan(&used)
	if _, err := deps.Pool.Exec(ctx, `UPDATE users SET storage_quota_bytes = $2 WHERE id = $1`, uid, used+1500); err != nil {
		t.Fatal(err)
	}
	defer deps.Pool.Exec(ctx, `UPDATE users SET storage_quota_bytes = $2 WHERE id = $1`, uid, quota)
	body := map[string]any{"name": "Q", "duration_ms": 1000, "byte_size": 1000, "format": "mp3", "sha256": sha256Hex("q")}
	first := expect(t, c.mutate("POST", "/me/tracks", body), 201)
	// The second sync would pass the quota with the first one reserved (R-LIM-5).
	if r := c.mutate("POST", "/me/tracks", body); r.code() != "storage_quota_exceeded" {
		t.Fatalf("over-quota sync: %s", r.Raw)
	}
	expect(t, c.mutate("DELETE", "/me/tracks/"+first["track"].(map[string]any)["id"].(string)+"?reason=cancel_pending", nil), 200)
	second := expect(t, c.mutate("POST", "/me/tracks", body), 201)
	expect(t, c.mutate("DELETE", "/me/tracks/"+second["track"].(map[string]any)["id"].(string)+"?reason=cancel_pending", nil), 200)
}

func sha256Hex(s string) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, 64)
	for i := range out {
		out[i] = hexd[(int(s[0])+i)%16]
	}
	return string(out)
}
