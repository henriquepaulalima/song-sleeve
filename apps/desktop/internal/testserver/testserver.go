// Package testserver starts the real API for the desktop integration tests: built
// from ../server, with its workers, against the songsleeve_test database and the
// local S3 from docker-compose.
package testserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Main runs the tests against a fresh API process and exits. Accounts in
// allowlist get cloud sync. baseURL receives the API root (…/api).
func Main(m *testing.M, allowlist []string, baseURL *string) {
	ctx := context.Background()
	base := "postgres://songsleeve:songsleeve@localhost:55432/songsleeve?sslmode=disable"
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		fmt.Println("skipping: database unavailable:", err)
		os.Exit(0)
	}
	if _, err := conn.Exec(ctx, `CREATE DATABASE songsleeve_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
		panic(err)
	}
	conn.Close(ctx)
	testDB := strings.Replace(base, "/songsleeve?", "/songsleeve_test?", 1)
	if c, err := pgx.Connect(ctx, testDB); err == nil {
		_, _ = c.Exec(ctx, `DELETE FROM rate_limit_buckets`)
		c.Close(ctx)
	}

	// SeaweedFS takes a few seconds after the container starts.
	for i := 0; i < 60; i++ {
		if res, err := http.Get("http://localhost:58333/"); err == nil {
			res.Body.Close()
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	_, here, _, _ := runtime.Caller(0)
	bin := filepath.Join(os.TempDir(), "songsleeve-api-test")
	build := exec.Command("go", "build", "-o", bin, "./cmd/api")
	build.Dir = filepath.Join(filepath.Dir(here), "../../../../server")
	if out, err := build.CombinedOutput(); err != nil {
		panic(string(out))
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(bin, "-migrate", "-with-worker")
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+testDB, fmt.Sprintf("PORT=%d", port),
		"CLOUD_SYNC_ALLOWLIST="+strings.Join(allowlist, ","), "STALL_TIMEOUT=20s", "UPLOAD_GRANT_TTL=2s",
	)
	logf, _ := os.CreateTemp("", "songsleeve-api-*.log")
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	*baseURL = fmt.Sprintf("http://127.0.0.1:%d/api", port)
	healthy := false
	for i := 0; i < 300 && !healthy; i++ {
		if res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port)); err == nil {
			healthy = res.StatusCode == 200
			res.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !healthy {
		fmt.Println("the API did not start; log:", logf.Name())
		os.Exit(1)
	}
	code := m.Run()
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()
	if code != 0 {
		fmt.Println("API log:", logf.Name())
	}
	os.Exit(code)
}
