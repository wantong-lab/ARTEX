package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoadMigrationsSorted(t *testing.T) {
	fsys := fstest.MapFS{
		"m/0002_second.sql": {Data: []byte("SELECT 2;")},
		"m/0001_first.sql":  {Data: []byte("SELECT 1;")},
		"m/notes.md":        {Data: []byte("ignored")},
		"m/sub/0003.sql":    {Data: []byte("SELECT 3;")},
	}
	migs, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) != 2 {
		t.Fatalf("migrations=%d, want 2 (non-sql and subdirs ignored)", len(migs))
	}
	if migs[0].version != "0001_first" || migs[1].version != "0002_second" {
		t.Fatalf("order=%v", []string{migs[0].version, migs[1].version})
	}
	if migs[0].sql != "SELECT 1;" {
		t.Fatalf("content=%q", migs[0].sql)
	}
}

func TestBaselineContainsCoreTables(t *testing.T) {
	baseline := baselineSQL()
	if strings.TrimSpace(baseline) == "" {
		t.Fatal("baseline SQL is empty")
	}
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS assets",
		"CREATE TABLE IF NOT EXISTS settings",
		"CREATE TABLE IF NOT EXISTS tasks",
	} {
		if !strings.Contains(baseline, needle) {
			t.Fatalf("baseline missing %q", needle)
		}
	}
}

func TestLegacySchemaEnabled(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"off":   false,
		"1":     true,
		"TRUE":  true,
		"  on ": true,
	}
	for value, want := range cases {
		t.Setenv(legacySchemaEnv, value)
		if got := legacySchemaEnabled(); got != want {
			t.Fatalf("legacySchemaEnabled(%q)=%v, want %v", value, got, want)
		}
	}
}

// openMigrationTestDB 建一个隔离数据库，测试结束自动删除，避免污染共享测试库。
func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn, _, err := DSN()
	if err != nil {
		t.Skipf("no test database configured: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Skipf("unparseable dsn: %v", err)
	}
	adminURL := *u
	adminURL.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		t.Skipf("open admin connection: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Skipf("postgres unreachable: %v", err)
	}
	name := fmt.Sprintf("artex_migtest_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		_ = admin.Close()
		t.Skipf("cannot create test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`)
		_ = admin.Close()
	})
	targetURL := *u
	targetURL.Path = "/" + name
	sqlDB, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return sqlDB
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	sqlDB := openMigrationTestDB(t)
	ctx := t.Context()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	noSleep := func(time.Duration) {}

	first, err := applyMigrations(ctx, conn, noSleep)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if first != 6 {
		t.Fatalf("first apply=%d, want 6 (baseline + llm + honeypot + inner net + pending scope + memory)", first)
	}
	for _, table := range []string{"assets", "settings", "tasks"} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s missing after baseline", table)
		}
	}
	var recorded int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if recorded != 6 {
		t.Fatalf("schema_migrations rows=%d, want 6", recorded)
	}

	second, err := applyMigrations(ctx, conn, noSleep)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if second != 0 {
		t.Fatalf("second apply=%d, want 0 (idempotent)", second)
	}
}
