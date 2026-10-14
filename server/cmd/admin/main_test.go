//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
)

// Runs against the integration test database (created by the e2e tests or here).
func testDB(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	base := "postgres://songsleeve:songsleeve@localhost:55432/songsleeve?sslmode=disable"
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Skip("database unavailable:", err)
		}
		if _, err := conn.Exec(ctx, `CREATE DATABASE songsleeve_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
			t.Fatal(err)
		}
		conn.Close(ctx)
		url = strings.Replace(base, "/songsleeve?", "/songsleeve_test?", 1)
	}
	t.Setenv("DATABASE_URL", url)
	pool, err := db.Open(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	return conn
}

func TestAdminCommands(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	t.Setenv("ADMIN_OPERATOR", "test-operator")
	id, _ := uuid.NewV7()
	email := fmt.Sprintf("admin-%s@example.com", uuid.NewString()[:8])
	if _, err := conn.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, terms_accepted_at, terms_version, storage_quota_bytes)
		VALUES ($1, $2, 'x', now(), 'v', 100)`, id, email); err != nil {
		t.Fatal(err)
	}
	devID, _ := uuid.NewV7()
	sessID, _ := uuid.NewV7()
	if _, err := conn.Exec(ctx, `INSERT INTO devices (id, user_id, installation_id, platform, label) VALUES ($1, $2, 'i', 'desktop', 'PC')`, devID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_sessions (id, user_id, device_id, token_hash, transport, expires_at) VALUES ($1, $2, $3, $4, 'bearer', now() + interval '1 day')`, sessID, id, devID, []byte(sessID.String())); err != nil {
		t.Fatal(err)
	}

	steps := [][]string{
		{"usage", email},
		{"set-quota", email, "5000"},
		{"cloud-sync", email, "on"},
		{"suspend", email},
	}
	for _, args := range steps {
		if err := run(ctx, args); err != nil {
			t.Fatalf("admin %v: %v", args, err)
		}
	}
	var quota int64
	var cloud bool
	var suspended *string
	if err := conn.QueryRow(ctx, `SELECT storage_quota_bytes, cloud_sync_enabled, suspended_at::text FROM users WHERE id = $1`, id).Scan(&quota, &cloud, &suspended); err != nil {
		t.Fatal(err)
	}
	if quota != 5000 || !cloud || suspended == nil {
		t.Fatalf("quota %d cloud %v suspended %v", quota, cloud, suspended)
	}
	var revoked bool
	_ = conn.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM user_sessions WHERE id = $1`, sessID).Scan(&revoked)
	if !revoked {
		t.Fatal("suspension must revoke every session")
	}
	if err := run(ctx, []string{"unsuspend", email}); err != nil {
		t.Fatal(err)
	}
	// Every command writes the audit log; reads (usage) don't.
	var actions []string
	rows, err := conn.Query(ctx, `SELECT action FROM admin_actions WHERE user_id = $1 AND operator = 'test-operator' ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		actions = append(actions, a)
	}
	if strings.Join(actions, ",") != "set_quota,cloud_sync,suspend,unsuspend" {
		t.Fatalf("audit log: %v", actions)
	}
	if err := run(ctx, []string{"set-quota", email, "lots"}); err == nil {
		t.Fatal("a non-numeric quota was accepted")
	}
	if err := run(ctx, []string{"usage", "nobody@example.com"}); err == nil {
		t.Fatal("unknown account accepted")
	}
	if err := run(ctx, []string{"stalled"}); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"delete-account", email}); err != nil {
		t.Fatal(err)
	}
	var deleting bool
	_ = conn.QueryRow(ctx, `SELECT deletion_requested_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&deleting)
	if !deleting {
		t.Fatal("delete-account did not start the deletion")
	}
	if err := run(ctx, []string{"delete-account", email}); err == nil {
		t.Fatal("a second deletion was accepted")
	}
}
