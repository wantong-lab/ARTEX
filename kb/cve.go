package kb

// CVE 语料库：兼容原版 0.4.2 cve / cve_fts 表（20.3 万条）与 cves 表。

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type CVEClient struct {
	db     *sql.DB
	hasCVE bool // true = 原版 cve(id,year,vendor,products,published,desc,refs) + cve_fts
}

func OpenCVE(dir string) (*CVEClient, error) {
	path := filepath.Join(dir, "cve.db")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("cve.db 不存在: %s", path)
	}
	// 正常读写打开(WAL 会写 -wal/-shm);data/redteam 若以 :ro 挂载会
	// SQLITE_CANTOPEN(14) —— cve_search 报 "unable to open database file (14)"。
	// 退回 immutable 只读模式:不加锁、不写任何旁路文件,适合只读语料库。
	db, err := openCVESQLite(fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path))
	readOnly := false
	if err != nil {
		db, err = openCVESQLite(fmt.Sprintf("file:%s?immutable=1&_pragma=busy_timeout(5000)", path))
		if err != nil {
			return nil, fmt.Errorf("cve.db 打开失败: %w", err)
		}
		readOnly = true
	}
	c := &CVEClient{db: db}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cve'`).Scan(&n)
	if n > 0 {
		c.hasCVE = true
		return c, nil
	}
	// 兼容旧库:有 cves 表就能直接检索;两张表都没有时才需要建表+种子,只读挂载下放弃写入。
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cves'`).Scan(&n)
	if n == 0 {
		if readOnly {
			return nil, fmt.Errorf("cve.db 只读且无 cve/cves 表: %s", path)
		}
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS cves(
			id TEXT PRIMARY KEY, year INTEGER, description TEXT,
			cwe TEXT, severity TEXT, has_exploit INTEGER DEFAULT 0, refs TEXT)`)
		c.seedBaselineIfEmpty()
	}
	return c, nil
}

// openCVESQLite 打开并探活,避免 database/sql 懒加载把打开错误推迟到首次查询。
func openCVESQLite(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	var one int
	if err := db.QueryRow(`SELECT 1`).Scan(&one); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (c *CVEClient) seedBaselineIfEmpty() {
	if n, err := c.Count(); err != nil || n > 0 {
		return
	}
	seeds := []CVERecord{
		{ID: "CVE-2021-44228", Year: 2021, Description: "Apache Log4j2 JNDI injection Remote Code Execution (Log4Shell).", CWE: "CWE-502", Severity: "CRITICAL", HasExploit: true, Refs: "https://nvd.nist.gov/vuln/detail/CVE-2021-44228"},
		{ID: "CVE-2023-46604", Year: 2023, Description: "Apache ActiveMQ OpenWire Deserialization RCE.", CWE: "CWE-502", Severity: "CRITICAL", HasExploit: true, Refs: "https://nvd.nist.gov/vuln/detail/CVE-2023-46604"},
	}
	for i := range seeds {
		_ = c.upsertCVE(&seeds[i])
	}
}

func (c *CVEClient) Close() error { return c.db.Close() }

type CVERecord struct {
	ID          string `json:"id"`
	Year        int    `json:"year"`
	Vendor      string `json:"vendor,omitempty"`
	Products    string `json:"products,omitempty"`
	Published   string `json:"published,omitempty"`
	Description string `json:"description"`
	Desc        string `json:"desc,omitempty"`
	CWE         string `json:"cwe,omitempty"`
	Severity    string `json:"severity,omitempty"`
	HasExploit  bool   `json:"has_exploit"`
	Refs        string `json:"refs,omitempty"`
}

func (c *CVEClient) Count() (int, error) {
	var n int
	if c.hasCVE {
		err := c.db.QueryRow(`SELECT COUNT(*) FROM cve`).Scan(&n)
		return n, err
	}
	err := c.db.QueryRow(`SELECT COUNT(*) FROM cves`).Scan(&n)
	return n, err
}

// Search：优先走原版 20.3w 条 cve_fts 全文索引（毫秒级），回退 LIKE，支持中英双语自动扩展。
func (c *CVEClient) Search(q string, limit int) ([]*CVERecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if c.hasCVE {
		return c.searchProdCVE(q, limit)
	}
	queries := ExpandBilingualQuery(q)
	if len(queries) == 0 {
		queries = []string{""}
	}
	var conds []string
	var args []any
	for _, sub := range queries {
		for _, tok := range strings.Fields(sub) {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			p := "%" + escapeLike(tok) + "%"
			conds = append(conds, `(id LIKE ? OR description LIKE ? OR cwe LIKE ?)`)
			args = append(args, p, p, p)
		}
	}
	var sqlStr string
	if len(conds) == 0 {
		sqlStr = `SELECT id,year,description,cwe,severity,has_exploit,refs FROM cves ORDER BY year DESC, id DESC LIMIT ?`
		args = []any{limit}
	} else {
		sqlStr = `SELECT id,year,description,cwe,severity,has_exploit,refs FROM cves WHERE ` +
			strings.Join(conds, " OR ") + ` ORDER BY has_exploit DESC, year DESC LIMIT ?`
		args = append(args, limit)
	}
	rows, err := c.db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CVERecord
	for rows.Next() {
		var r CVERecord
		var he int
		if err := rows.Scan(&r.ID, &r.Year, &r.Description, &r.CWE, &r.Severity, &he, &r.Refs); err == nil {
			r.Desc = r.Description
			r.HasExploit = he != 0
			out = append(out, &r)
		}
	}
	return out, rows.Err()
}

func (c *CVEClient) searchProdCVE(q string, limit int) ([]*CVERecord, error) {
	q = strings.TrimSpace(q)
	var rows *sql.Rows
	var err error
	if q == "" {
		rows, err = c.db.Query(`SELECT id,year,vendor,products,published,desc,refs FROM cve ORDER BY year DESC, id DESC LIMIT ?`, limit)
	} else if strings.HasPrefix(strings.ToUpper(q), "CVE-") && !strings.Contains(q, " ") {
		rows, err = c.db.Query(`SELECT id,year,vendor,products,published,desc,refs FROM cve WHERE id LIKE ? ORDER BY year DESC, id DESC LIMIT ?`, "%"+strings.ToUpper(q)+"%", limit)
	} else {
		queries := ExpandBilingualQuery(q)
		// Construct FTS5 OR expression from alphanumeric tokens
		var ftsTokens []string
		seen := map[string]bool{}
		for _, sub := range queries {
			for _, tok := range strings.Fields(sub) {
				tok = strings.TrimSpace(tok)
				if len(tok) >= 2 && !seen[strings.ToLower(tok)] {
					seen[strings.ToLower(tok)] = true
					ftsTokens = append(ftsTokens, ftsQuote(tok))
				}
			}
		}
		if len(ftsTokens) > 0 {
			ftsExpr := strings.Join(ftsTokens, " OR ")
			rows, err = c.db.Query(`SELECT c.id,c.year,c.vendor,c.products,c.published,c.desc,c.refs
				FROM cve_fts f JOIN cve c ON c.rowid = f.rowid
				WHERE cve_fts MATCH ? ORDER BY c.year DESC LIMIT ?`, ftsExpr, limit)
		}
		if err != nil || rows == nil {
			p := "%" + escapeLike(q) + "%"
			rows, err = c.db.Query(`SELECT id,year,vendor,products,published,desc,refs FROM cve
				WHERE id LIKE ? OR products LIKE ? OR desc LIKE ? ORDER BY year DESC LIMIT ?`, p, p, p, limit)
		}
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CVERecord
	for rows.Next() {
		var r CVERecord
		var vendor, prods, pub, desc, refs sql.NullString
		var year sql.NullInt64
		if err := rows.Scan(&r.ID, &year, &vendor, &prods, &pub, &desc, &refs); err == nil {
			r.Year = int(year.Int64)
			r.Vendor = vendor.String
			r.Products = prods.String
			r.Published = pub.String
			r.Description = desc.String
			r.Desc = desc.String
			r.Refs = refs.String
			r.CWE = prods.String
			r.HasExploit = strings.Contains(strings.ToLower(refs.String), "exploit") || strings.Contains(strings.ToLower(refs.String), "github.com") || strings.Contains(strings.ToLower(refs.String), "packetstorm")
			out = append(out, &r)
		}
	}
	return out, rows.Err()
}

// Years 语料覆盖年份范围。
func (c *CVEClient) Years() (minY, maxY int, err error) {
	if c.hasCVE {
		err = c.db.QueryRow(`SELECT COALESCE(MIN(year),0),COALESCE(MAX(year),0) FROM cve WHERE year>1990`).Scan(&minY, &maxY)
		return
	}
	err = c.db.QueryRow(`SELECT COALESCE(MIN(year),0),COALESCE(MAX(year),0) FROM cves`).Scan(&minY, &maxY)
	return
}

func ftsQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
