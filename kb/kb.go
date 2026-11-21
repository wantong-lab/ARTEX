// Package kb — ARTEX 知识库客户端（兼容原版 0.4.2 poc/poc_fts 表与 entries 表，弹药/打法/负面三档）。
package kb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Client struct {
	dir    string
	db     *sql.DB
	hasPoc bool
}

// Entry 知识条目（弹药/打法/负面分档）。
type Entry struct {
	ID          string `json:"id"`
	Code        string `json:"code,omitempty"`
	Title       string `json:"title"`
	Kind        string `json:"kind,omitempty"`
	Category    string `json:"category"`   // ammo|tactic|negative
	VulnClass   string `json:"vuln_class"` // rce|sqli|upload|deser|idor|...
	Product     string `json:"product"`
	CVE         string `json:"cve,omitempty"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content"`
	File        string `json:"file,omitempty"`
	Verified    bool   `json:"verified"`
	HitCount    int    `json:"hit_count,omitempty"`
	Score       int    `json:"score,omitempty"`
	RowID       int64  `json:"-"` // sqlite 数值行 id(memorist 水位线增量索引用)
}

// Open 打开 knowledge.db（不存在则建空库）。dir 为 redteam/ 根。
func Open(dir string) (*Client, error) {
	if dir == "" {
		return nil, fmt.Errorf("knowledge dir 为空")
	}
	path := filepath.Join(dir, "knowledge.db")
	open := func(uri string) (*Client, error) {
		db, err := sql.Open("sqlite", uri)
		if err != nil {
			return nil, err
		}
		c := &Client{dir: dir, db: db}
		if err := c.probe(); err != nil {
			_ = db.Close()
			return nil, err
		}
		return c, nil
	}
	// 正常路径：可写目录，开 WAL（并发读友好）。
	uri := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	if c, err := open(uri); err == nil {
		return c, nil
	} else if !os.IsNotExist(err) {
		// 只读挂载（容器里 knowledge.db 常以 :ro 挂进来）时 WAL 需要写 -shm/-wal，
		// 会直接报 SQLITE_CANTOPEN(14)。此时退回 immutable 只读模式：
		// 不加锁、不写任何旁路文件，适合只读知识库。
		uriRO := fmt.Sprintf("file:%s?immutable=1&_pragma=busy_timeout(5000)", path)
		if c, rerr := open(uriRO); rerr == nil {
			return c, nil
		}
		return nil, fmt.Errorf("knowledge.db 打开失败: %w", err)
	}
	return nil, fmt.Errorf("knowledge.db 不存在: %s", path)
}

func (c *Client) probe() error {
	var n int
	_ = c.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='poc'`).Scan(&n)
	if n > 0 {
		c.hasPoc = true
		return nil
	}
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS entries(
		id TEXT PRIMARY KEY, title TEXT, category TEXT,
		vuln_class TEXT, product TEXT, content TEXT, file TEXT)`)
	return err
}

func (c *Client) Dir() string  { return c.dir }
func (c *Client) Close() error { return c.db.Close() }

// classifyPocBucket 将原版 poc 表的 kind/category/title/tags 归入 ammo / tactic / negative 三档。
func classifyPocBucket(kind, cat, title, tags, desc string) string {
	combined := strings.ToLower(title + " " + tags + " " + desc)
	if strings.Contains(combined, "不可行") || strings.Contains(combined, "负面") ||
		strings.Contains(combined, "阴性") || strings.Contains(combined, "误报") ||
		strings.Contains(combined, "证伪") || strings.Contains(combined, "证否") ||
		strings.Contains(combined, "无法getshell") || strings.Contains(combined, "negative") ||
		strings.Contains(combined, "走不通") || strings.Contains(combined, "死胡同") {
		return "negative"
	}
	kl := strings.ToLower(kind)
	cl := strings.ToLower(cat)
	if kl == "methodology" || cl == "methodology" || cl == "app-audit" || cl == "env" || cl == "recon" || kl == "markdown" {
		return "tactic"
	}
	return "ammo"
}

// Get 按 id 或 code 取条目；file/path 相对路径时读 pocs/ 下正文。
func (c *Client) Get(id string) (*Entry, error) {
	if c.hasPoc {
		var (
			nid                                                   int64
			code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
			verified, hits                                        sql.NullInt64
		)
		err := c.db.QueryRow(`SELECT id,code,title,kind,category,cve,component,description,content,path,verified,hit_count
			FROM poc WHERE code=? OR CAST(id AS TEXT)=? LIMIT 1`, id, id).
			Scan(&nid, &code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &verified, &hits)
		if err == nil {
			body := content.String
			if body == "" && pth.String != "" {
				if b, rerr := c.readPocFile(pth.String); rerr == nil {
					body = string(b)
				}
			}
			if desc.String != "" && !strings.Contains(body, desc.String) {
				if body != "" {
					body = desc.String + "\n\n" + body
				} else {
					body = desc.String
				}
			}
			e := &Entry{
				ID:          nz(code.String, fmt.Sprint(nid)),
				Code:        nz(code.String, fmt.Sprint(nid)),
				Title:       title.String,
				Kind:        kind.String,
				Category:    classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
				VulnClass:   cat.String,
				Product:     nz(comp.String, cve.String),
				CVE:         cve.String,
				Description: desc.String,
				Content:     body,
				File:        pth.String,
				Verified:    verified.Int64 != 0,
				HitCount:    int(hits.Int64),
			}
			return e, nil
		}
	}
	var e Entry
	err := c.db.QueryRow(`SELECT id,title,category,vuln_class,product,content,file FROM entries WHERE id=?`, id).
		Scan(&e.ID, &e.Title, &e.Category, &e.VulnClass, &e.Product, &e.Content, &e.File)
	if err != nil {
		return nil, err
	}
	e.Code = e.ID
	if e.File != "" && e.Content == "" {
		if b, err := c.readPocFile(e.File); err == nil {
			e.Content = string(b)
		}
	}
	return &e, nil
}

func nz(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// resolvePath 限制在 redteam/ 或 pocs/ 内防穿越。
func (c *Client) resolvePath(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("非法路径 %q", rel)
	}
	if strings.HasPrefix(clean, "pocs/") || strings.HasPrefix(clean, "pocs\\") {
		return filepath.Join(c.dir, clean), nil
	}
	return filepath.Join(c.dir, "pocs", clean), nil
}

func (c *Client) readPocFile(rel string) ([]byte, error) {
	p, err := c.resolvePath(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Stats 库统计：条目数/按类别（兼容 poc 表与 entries 表）。
func (c *Client) Stats() (map[string]int, error) {
	out := map[string]int{}
	if c.hasPoc {
		var total, ver, reused, neg int
		_ = c.db.QueryRow(`SELECT COUNT(*) FROM poc`).Scan(&total)
		_ = c.db.QueryRow(`SELECT COUNT(*) FROM poc WHERE verified=1`).Scan(&ver)
		_ = c.db.QueryRow(`SELECT COALESCE(SUM(hit_count),0) FROM poc`).Scan(&reused)
		_ = c.db.QueryRow(`SELECT COUNT(*) FROM poc WHERE `+negativeWhere).Scan(&neg)
		var tac int
		_ = c.db.QueryRow(`SELECT COUNT(*) FROM poc WHERE `+tacticWhere+` AND NOT `+negativeWhere).Scan(&tac)
		out["tactic"] = tac
		out["ammo"] = total - neg - tac
		out["negative"] = neg
		out["_total"] = total
		out["_verified"] = ver
		out["_reused"] = reused
		return out, nil
	}
	rows, err := c.db.Query(`SELECT category, COUNT(*) FROM entries GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err == nil {
			out[cat] = n
		}
	}
	return out, rows.Err()
}

// markUsed 条目命中计数（弹药档排序用）。
func (c *Client) markUsed(id string) {
	if c.hasPoc {
		_, _ = c.db.Exec(`UPDATE poc SET hit_count = COALESCE(hit_count,0)+1 WHERE code=?`, id)
		return
	}
	_, _ = c.db.Exec(`CREATE TABLE IF NOT EXISTS usage(id TEXT PRIMARY KEY, n INTEGER)`)
	_, _ = c.db.Exec(`INSERT INTO usage(id,n) VALUES(?,1)
		ON CONFLICT(id) DO UPDATE SET n=n+1`, id)
}

// GuessCategory 按标题/正文特征猜三档：弹药/打法/负面。
func GuessCategory(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "不可行") || strings.Contains(t, "排除") ||
		strings.Contains(t, "走不通") || strings.Contains(t, "别再走") ||
		strings.Contains(t, "负面") || strings.Contains(t, "误报"):
		return "negative"
	case strings.Contains(t, "poc") || strings.Contains(t, "exp") ||
		strings.Contains(t, "payload") || strings.Contains(t, "复现"):
		return "ammo"
	default:
		return "tactic"
	}
}

// GuessPolarity 条目极性（正/负情报）。
func GuessPolarity(category string) int {
	if category == "negative" {
		return -1
	}
	return 1
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func within(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func slugCode(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r >= 0x4e00 && r <= 0x9fa5 {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	res := strings.Trim(b.String(), "-")
	if rs := []rune(res); len(rs) > 48 {
		res = string(rs[:48])
	}
	return res
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// negativeWhere 与 classifyPocBucket 的负面判定同语义的关键词 SQL 片段
// （title/tags/description 任一命中即负面）。
const negativeWhere = `(lower(COALESCE(title,'')) LIKE '%不可行%' OR lower(COALESCE(title,'')) LIKE '%负面%'
	OR lower(COALESCE(title,'')) LIKE '%阴性%' OR lower(COALESCE(title,'')) LIKE '%误报%'
	OR lower(COALESCE(title,'')) LIKE '%证伪%' OR lower(COALESCE(title,'')) LIKE '%证否%'
	OR lower(COALESCE(title,'')) LIKE '%无法getshell%' OR lower(COALESCE(title,'')) LIKE '%走不通%'
	OR lower(COALESCE(title,'')) LIKE '%死胡同%' OR lower(COALESCE(tags,'')) LIKE '%negative%'
	OR lower(COALESCE(description,'')) LIKE '%不可行%' OR lower(COALESCE(description,'')) LIKE '%负面%'
	OR lower(COALESCE(description,'')) LIKE '%阴性%' OR lower(COALESCE(description,'')) LIKE '%误报%'
	OR lower(COALESCE(description,'')) LIKE '%证伪%' OR lower(COALESCE(description,'')) LIKE '%证否%'
	OR lower(COALESCE(description,'')) LIKE '%无法getshell%' OR lower(COALESCE(description,'')) LIKE '%走不通%'
	OR lower(COALESCE(description,'')) LIKE '%死胡同%' OR lower(COALESCE(description,'')) LIKE '%negative%')`

// tacticWhere 与 classifyPocBucket 的打法判定同语义：kind/category 命中且非负面。
const tacticWhere = `(lower(COALESCE(kind,''))='methodology' OR lower(COALESCE(kind,''))='markdown'
	OR lower(COALESCE(category,'')) IN ('methodology','app-audit','env','recon'))`

// bucketWhere 返回 ammo/tactic/negative 三档的 WHERE 片段（与 classifyPocBucket 同语义）。
func bucketWhere(bucket string) string {
	switch bucket {
	case "negative":
		return negativeWhere
	case "tactic", "playbook":
		return tacticWhere + " AND NOT " + negativeWhere
	default:
		return "NOT " + negativeWhere + " AND NOT (" + tacticWhere + ")"
	}
}

// ListByCategory 按 ammo/tactic/negative 单档分页列举（poc 表路径），不再让
// id DESC LIMIT 截断把整档条目切没——之前按全表 LIMIT 再分桶，老导入的打法
// 都在低 id 段，UI 永远只能看到窗口内的两三条。
func (c *Client) ListByCategory(bucket string, limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 200
	}
	if !c.hasPoc {
		return c.listByCategoryEntries(bucket, limit)
	}
	rows, err := c.db.Query(`SELECT code,title,kind,category,cve,component,description,content,path,verified,hit_count
		FROM poc WHERE `+bucketWhere(bucket)+` ORDER BY hit_count DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		var code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
		var ver, hits sql.NullInt64
		if err := rows.Scan(&code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &ver, &hits); err == nil {
			body := content.String
			if desc.String != "" {
				body = desc.String + "\n" + body
			}
			out = append(out, &Entry{
				ID: code.String, Code: code.String, Title: title.String, Kind: kind.String,
				Category:    bucket,
				VulnClass:   cat.String,
				Product:     nz(comp.String, cve.String),
				CVE:         cve.String,
				Description: desc.String,
				Content:     truncateRunes(body, 1500),
				File:        pth.String,
				Verified:    ver.Int64 != 0,
				HitCount:    int(hits.Int64),
			})
		}
	}
	return out, rows.Err()
}

// listByCategoryEntries 是 entries 表路径的兜底（老库，无 poc 表）。
func (c *Client) listByCategoryEntries(bucket string, limit int) ([]*Entry, error) {
	cat := bucket
	if cat == "playbook" {
		cat = "tactic"
	}
	rows, err := c.db.Query(`SELECT id,title,category,vuln_class,product,content,file
		FROM entries WHERE category=? ORDER BY id LIMIT ?`, cat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e := &Entry{}
		var vc, prod, file sql.NullString
		if err := rows.Scan(&e.ID, &e.Title, &e.Category, &vc, &prod, &e.Content, &file); err == nil {
			e.VulnClass, e.Product, e.File = vc.String, prod.String, file.String
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// ListAll 全量迭代知识条目（memorist daemon 灌向量索引用）。
func (c *Client) ListAll(limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 1000
	}
	if c.hasPoc {
		rows, err := c.db.Query(`SELECT code,title,kind,category,cve,component,description,content,path,verified,hit_count
			FROM poc ORDER BY id DESC LIMIT ?`, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*Entry
		for rows.Next() {
			var code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
			var ver, hits sql.NullInt64
			if err := rows.Scan(&code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &ver, &hits); err == nil {
				body := content.String
				if desc.String != "" {
					body = desc.String + "\n" + body
				}
				out = append(out, &Entry{
					ID:          code.String,
					Code:        code.String,
					Title:       title.String,
					Kind:        kind.String,
					Category:    classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
					VulnClass:   cat.String,
					Product:     nz(comp.String, cve.String),
					CVE:         cve.String,
					Description: desc.String,
					Content:     truncateRunes(body, 1500),
					File:        pth.String,
					Verified:    ver.Int64 != 0,
					HitCount:    int(hits.Int64),
				})
			}
		}
		return out, rows.Err()
	}
	return c.listAllEntries(limit)
}

// ExistsByContent 报告 poc 表内是否已有完全相同的 content（收割脚本/成功命令去重）。
func (c *Client) ExistsByContent(content string) bool {
	if !c.hasPoc || content == "" {
		return false
	}
	var n int
	if err := c.db.QueryRow(`SELECT 1 FROM poc WHERE content=? LIMIT 1`, content).Scan(&n); err == nil {
		return n == 1
	}
	return false
}

// ListTopHit 按 hit_count 降序取复用榜（KB 面板 top_reused）。
func (c *Client) ListTopHit(limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	if !c.hasPoc {
		return c.ListAll(limit)
	}
	rows, err := c.db.Query(`SELECT code,title,kind,category,cve,component,description,content,path,verified,hit_count
		FROM poc ORDER BY hit_count DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		var code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
		var ver, hits sql.NullInt64
		if err := rows.Scan(&code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &ver, &hits); err == nil {
			body := content.String
			if desc.String != "" {
				body = desc.String + "\n" + body
			}
			out = append(out, &Entry{
				ID: code.String, Code: code.String, Title: title.String, Kind: kind.String,
				Category: classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
				VulnClass: cat.String, Product: nz(comp.String, cve.String), CVE: cve.String,
				Description: desc.String, Content: truncateRunes(body, 1500),
				File: pth.String, Verified: ver.Int64 != 0, HitCount: int(hits.Int64),
			})
		}
	}
	return out, rows.Err()
}

// ListSinceID 按 sqlite 数值 id 升序取 afterID 之后的条目(memorist 水位线增量索引,
// 保证存量自动追平且不重复灌已索引条目)。
func (c *Client) ListSinceID(afterID int64, limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 300
	}
	if !c.hasPoc {
		return c.ListAll(limit)
	}
	rows, err := c.db.Query(`SELECT id,code,title,kind,category,cve,component,description,content,path,verified,hit_count
		FROM poc WHERE id>? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		var nid int64
		var code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
		var ver, hits sql.NullInt64
		if err := rows.Scan(&nid, &code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &ver, &hits); err == nil {
			body := content.String
			if desc.String != "" {
				body = desc.String + "\n" + body
			}
			out = append(out, &Entry{
				ID:          nz(code.String, fmt.Sprint(nid)),
				Code:        code.String,
				Title:       title.String,
				Kind:        kind.String,
				Category:    classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
				VulnClass:   cat.String,
				Product:     nz(comp.String, cve.String),
				CVE:         cve.String,
				Description: desc.String,
				Content:     truncateRunes(body, 1500),
				File:        pth.String,
				Verified:    ver.Int64 != 0,
				HitCount:    int(hits.Int64),
				RowID:       nid,
			})
		}
	}
	return out, rows.Err()
}

func (c *Client) listAllEntries(limit int) ([]*Entry, error) {
	rows, err := c.db.Query(`SELECT id,title,category,vuln_class,product,content,file
		FROM entries ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e := &Entry{}
		var file *string
		if err := rows.Scan(&e.ID, &e.Title, &e.Category, &e.VulnClass, &e.Product, &e.Content, &file); err == nil {
			e.Code = e.ID
			if file != nil {
				e.File = *file
			}
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
