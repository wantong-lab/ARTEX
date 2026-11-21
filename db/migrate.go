package db

// 迁移体系：db/migrations/*.sql 按文件名排序执行，已应用版本记录在
// schema_migrations。基线 0001_baseline.sql 就是原 schema.sql（幂等 DDL）：
// 历史库（有表但没有迁移记录）首次启动会把它当无操作执行并登记，与旧行为等价。
//
// 保留 ARTEX_LEGACY_SCHEMA=1 回退：每次启动直接执行基线，不读写
// schema_migrations，供升级出问题时临时降级，一个版本周期后移除。

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// legacySchemaEnv 设置后启用旧路径（每次启动直接执行基线 DDL）。
const legacySchemaEnv = "ARTEX_LEGACY_SCHEMA"

// migration 是一个迁移单元。version 是文件名去掉 .sql（零填充保证字典序等于时间序）。
type migration struct {
	version string
	sql     string
}

// loadMigrations 读取 dir 下全部 .sql 并按 version 升序返回。非 .sql 文件忽略。
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version := strings.TrimSuffix(e.Name(), ".sql")
		if version == "" {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: version, sql: string(content)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

var (
	baselineOnce sync.Once
	baselineText string
	baselineErr  error
)

// baselineSQL 返回 0001 基线迁移的 SQL（旧路径与兼容测试共用）。
// 迁移文件在编译期固定，缺失属于程序缺陷，因此这里 panic 而不是静默空跑。
func baselineSQL() string {
	baselineOnce.Do(func() {
		migs, err := loadMigrations(migrationsFS, migrationsDir)
		if err != nil {
			baselineErr = err
			return
		}
		for _, m := range migs {
			if strings.HasPrefix(m.version, "0001_") {
				baselineText = m.sql
				return
			}
		}
		baselineErr = fmt.Errorf("baseline migration 0001_* not found")
	})
	if baselineErr != nil {
		panic("db: " + baselineErr.Error())
	}
	return baselineText
}

// legacySchemaEnabled 报告是否启用旧 schema 路径。
func legacySchemaEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(legacySchemaEnv))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// migrationConn 是迁移所需的最小连接能力（*sql.Conn 满足）。
type migrationConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

const createSchemaMigrationsSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// applyMigrations 在调用方持有迁移 advisory lock 的前提下执行未应用的迁移，
// 返回本次实际应用的数量。每个迁移在独立事务内执行并登记版本。
func applyMigrations(ctx context.Context, conn migrationConn, sleep func(time.Duration)) (int, error) {
	migs, err := loadMigrations(migrationsFS, migrationsDir)
	if err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, createSchemaMigrationsSQL); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		if err := applyOneMigration(ctx, conn, m, sleep); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// appliedVersions 读出已应用版本集合。
func appliedVersions(ctx context.Context, conn migrationConn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return out, nil
}

// applyOneMigration 在事务内执行单个迁移并登记版本；死锁按既有退避重试。
func applyOneMigration(ctx context.Context, conn migrationConn, m migration, sleep func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			if isPostgresDeadlock(err) && attempt < len(schemaDeadlockRetryDelays) {
				sleep(schemaDeadlockRetryDelays[attempt])
				continue
			}
			return fmt.Errorf("migration %s: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.version, err)
		}
		return nil
	}
}