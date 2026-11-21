package db

// schema 覆盖检查：确认迁移建出的表覆盖代码里 SQL 实际引用的表，以及核心表与
// 关键列存在。背景是干净实例事故：代码查表但 schema 没建，接口直接
// relation does not exist。检查分三层：
//  1. 纯逻辑：从迁移 DDL 解析表名，用 go/parser 只扫描字符串字面量里的 SQL，
//     求差集；不依赖数据库，任何环境都能跑。
//  2. 固定清单：核心表必须在迁移里声明（漏删/漏加一眼可见）。
//  3. 真库验证（有 DSN 才跑）：应用迁移到隔离库，核对表与关键列。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	createTableRe    = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+([a-z_][a-z0-9_]*)`)
	sqlStmtStartRe   = regexp.MustCompile(`(?s)^\s*(?:SELECT|INSERT|UPDATE|DELETE|WITH|MERGE)\b`)
	sqlStmtLineRe    = regexp.MustCompile(`(?s)(?:^|\n)\s*(?:SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	tableRefRe       = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|DELETE\s+FROM)\s+(?:"?[a-z_][a-z0-9_]*"?\.)?"?([a-z_][a-z0-9_]*)"?`)
	cteRe            = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s*(?:\([^()]*\))?\s+AS\s*(?:MATERIALIZED\s*)?\(`)
	sqlCommentLineRe = regexp.MustCompile(`(?m)--[^\n]*`)
	sqlCommentBlockRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlFuncFromRe    = regexp.MustCompile(`(?i)\b(?:EXTRACT|SUBSTRING|TRIM|OVERLAY)\s*\([^()]*\)`)
)

// nonTableTokens 是非表 token：SQL 片段、函数参数与提示文案里会出现的词，
// 例如 EXTRACT(EPOCH FROM created_at)、max(...)，以及以 UPDATE/INSERT 开头的英文提示。
// 新增条目必须写清来源，避免把真正的缺表名顺手加进来。
var nonTableTokens = map[string]bool{
	"select": true, "values": true, "lateral": true, "unnest": true,
	"generate_series": true, "json_populate_record": true, "json_populate_recordset": true,
	"jsonb_array_elements": true, "jsonb_array_elements_text": true, "jsonb_to_recordset": true,
	"information_schema": true, "pg_catalog": true, "pg_database": true,
	"only": true, "set": true, "where": true, "group": true, "order": true, "limit": true,
	"created_at": true, // EXTRACT(EPOCH FROM created_at)
	"max": true, "of": true, "skip": true, // SQL 片段/英文提示文案
}

// runtimeCreatedTables 是代码运行时建表（T-01）；M0-04 收敛进迁移后删除。
var runtimeCreatedTables = map[string]bool{"llm_records": true, "llm_usage": true}

// infraTables 由迁移框架自身创建，不在基线 DDL 里。
var infraTables = map[string]bool{"schema_migrations": true}

// coreTables 是必须由迁移声明的核心表；新增核心表时同步补充。
var coreTables = []string{
	"tasks", "assets", "companies", "company_scope", "explorations",
	"exploration_nodes", "exploration_edges", "findings", "settings",
	"llm_profiles", "activity", "tools", "agents", "agent_prompts",
	"intercept_rules", "task_scope", "task_categories",
}

// stripSQLNoise 去掉注释与带 FROM 参数的函数调用，减少误判。
func stripSQLNoise(s string) string {
	s = sqlCommentBlockRe.ReplaceAllString(s, " ")
	s = sqlCommentLineRe.ReplaceAllString(s, " ")
	s = sqlFuncFromRe.ReplaceAllString(s, " ")
	return s
}

// isSQLStatementLiteral 判断字符串字面量是否像 SQL 语句（而非英文提示）。
func isSQLStatementLiteral(s string) bool {
	return sqlStmtStartRe.MatchString(s) || sqlStmtLineRe.MatchString(s)
}

// createdTablesFromMigrations 汇总迁移 DDL 声明的表名。
func createdTablesFromMigrations(migs []migration) map[string]bool {
	out := map[string]bool{}
	for _, m := range migs {
		for _, match := range createTableRe.FindAllStringSubmatch(m.sql, -1) {
			out[strings.ToLower(match[1])] = true
		}
	}
	return out
}

// scanSQLTableRefs 解析 Go 源码字符串字面量里的 SQL，返回表引用与 CTE 名。
// 只看 db/ 与 server/ 的非测试文件；解析失败的文件跳过（不影响其他文件）。
func scanSQLTableRefs(root string) (map[string][]string, map[string]bool, error) {
	refs := map[string][]string{}
	ctes := map[string]bool{}
	for _, dir := range []string{"db", "server"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || !isSQLStatementLiteral(s) {
					return true
				}
				s = stripSQLNoise(s)
				for _, match := range cteRe.FindAllStringSubmatch(s, -1) {
					ctes[strings.ToLower(match[1])] = true
				}
				for _, match := range tableRefRe.FindAllStringSubmatch(s, -1) {
					table := strings.ToLower(match[1])
					refs[table] = append(refs[table], filepath.Base(path))
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return refs, ctes, nil
}

// tableCoverageGaps 返回代码引用但迁移未声明的表（排除 CTE 与非表 token）。
func tableCoverageGaps(created map[string]bool, refs map[string][]string, ctes map[string]bool) []string {
	var gaps []string
	for table := range refs {
		if created[table] || ctes[table] || nonTableTokens[table] || infraTables[table] || runtimeCreatedTables[table] {
			continue
		}
		gaps = append(gaps, table)
	}
	sort.Strings(gaps)
	return gaps
}

func TestTableCoverageGaps(t *testing.T) {
	created := map[string]bool{"tasks": true}
	refs := map[string][]string{"tasks": {"db/tasks.go"}, "ghost_table": {"db/tasks.go"}}
	gaps := tableCoverageGaps(created, refs, nil)
	if len(gaps) != 1 || gaps[0] != "ghost_table" {
		t.Fatalf("gaps=%v, want [ghost_table]", gaps)
	}
	if got := tableCoverageGaps(created, map[string][]string{"from_cte": {"db/x.go"}}, map[string]bool{"from_cte": true}); len(got) != 0 {
		t.Fatalf("CTE must not be reported as missing table: %v", got)
	}
}

func TestMigrationsCoverCodeTables(t *testing.T) {
	migs, err := loadMigrations(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	created := createdTablesFromMigrations(migs)
	refs, ctes, err := scanSQLTableRefs("..")
	if err != nil {
		t.Fatalf("scan sql refs: %v", err)
	}
	gaps := tableCoverageGaps(created, refs, ctes)
	if len(gaps) > 0 {
		for _, g := range gaps {
			t.Logf("missing table %q referenced by %v", g, refs[g])
		}
		t.Fatalf("migrations do not create %d table(s) referenced by code", len(gaps))
	}
}

func TestCoreTablesDeclared(t *testing.T) {
	migs, err := loadMigrations(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	created := createdTablesFromMigrations(migs)
	var missing []string
	for _, table := range coreTables {
		if !created[table] {
			missing = append(missing, table)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("core tables missing from migrations: %v", missing)
	}
}

func TestCoreTablesAndColumns(t *testing.T) {
	sqlDB := openMigrationTestDB(t)
	ctx := t.Context()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := applyMigrations(ctx, conn, func(time.Duration) {}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	checks := map[string][]string{
		"tasks":             {"id", "goal", "status"},
		"findings":          {"id", "task_id", "severity"},
		"settings":          {"key", "value"},
		"exploration_nodes": {"id", "payload", "state"},
	}
	for table, columns := range checks {
		for _, column := range columns {
			var exists bool
			if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`, table, column).Scan(&exists); err != nil {
				t.Fatalf("check %s.%s: %v", table, column, err)
			}
			if !exists {
				t.Fatalf("column %s.%s missing", table, column)
			}
		}
	}
}