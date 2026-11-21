package kb

// 知识条目管理（P1-8）：服务端分页、编辑、删除与 SQL 聚合统计。
// 兼容原版 poc 表与老 entries 表两条路径。

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// EntryPage 是服务端分页结果。
type EntryPage struct {
	Entries []*Entry `json:"entries"`
	Total   int      `json:"total"`
	Offset  int      `json:"offset"`
	Limit   int      `json:"limit"`
}

// Insights 是知识库聚合统计（SQL GROUP BY，不再取前 500 条内存聚合）。
type Insights struct {
	Total       int              `json:"total"`
	ByCategory  map[string]int   `json:"by_category"`
	ByVulnClass map[string]int   `json:"by_vuln_class"`
	ByKind      map[string]int   `json:"by_kind,omitempty"`
	Recent      []map[string]any `json:"recent"`
}

func normalizeBucket(bucket string) string {
	b := strings.ToLower(strings.TrimSpace(bucket))
	if b == "ammo" || b == "tactic" || b == "negative" || b == "playbook" {
		if b == "playbook" {
			return "tactic"
		}
		return b
	}
	return ""
}

// ListPage 服务端分页（category 可选：ammo/tactic/negative）。
func (c *Client) ListPage(offset, limit int, bucket string) (*EntryPage, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	bucket = normalizeBucket(bucket)
	if c.hasPoc {
		where := ""
		if bucket != "" {
			where = " WHERE " + bucketWhere(bucket)
		}
		var total int
		if err := c.db.QueryRow("SELECT COUNT(*) FROM poc" + where).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := c.db.Query("SELECT code,title,kind,category,cve,component,description,content,path,verified,hit_count FROM poc"+
			where+" ORDER BY id DESC LIMIT ? OFFSET ?", limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := make([]*Entry, 0, limit)
		for rows.Next() {
			var code, title, kind, cat, cve, comp, desc, content, pth sql.NullString
			var ver, hits sql.NullInt64
			if err := rows.Scan(&code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &ver, &hits); err != nil {
				return nil, err
			}
			body := content.String
			if desc.String != "" {
				body = desc.String + " " + body
			}
			out = append(out, &Entry{
				ID: nz(code.String, ""), Code: code.String, Title: title.String, Kind: kind.String,
				Category:  classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
				VulnClass: cat.String, Product: nz(comp.String, cve.String), CVE: cve.String,
				Description: desc.String, Content: truncateRunes(body, 3000), File: pth.String,
				Verified: ver.Int64 != 0, HitCount: int(hits.Int64),
			})
		}
		return &EntryPage{Entries: out, Total: total, Offset: offset, Limit: limit}, rows.Err()
	}
	where := ""
	if bucket != "" {
		where = " WHERE category=? "
	}
	var total int
	if where != "" {
		if err := c.db.QueryRow("SELECT COUNT(*) FROM entries"+where, bucket).Scan(&total); err != nil {
			return nil, err
		}
	} else if err := c.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&total); err != nil {
		return nil, err
	}
	q := "SELECT id,title,category,vuln_class,product,content,file FROM entries" + where + " ORDER BY id LIMIT ? OFFSET ?"
	args := []any{limit, offset}
	if where != "" {
		args = []any{bucket, limit, offset}
	}
	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Entry, 0, limit)
	for rows.Next() {
		e := &Entry{}
		var vc, prod, file sql.NullString
		if err := rows.Scan(&e.ID, &e.Title, &e.Category, &vc, &prod, &e.Content, &file); err != nil {
			return nil, err
		}
		e.VulnClass, e.Product, e.File = vc.String, prod.String, file.String
		out = append(out, e)
	}
	return &EntryPage{Entries: out, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

// Update 编辑条目（按 id/code）。poc 表更新后同步 FTS。
func (c *Client) Update(e *Entry) error {
	if e == nil || strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("id 必填")
	}
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("title 必填")
	}
	if c.hasPoc {
		kind := e.Kind
		if kind == "" {
			switch e.Category {
			case "tactic", "playbook":
				kind = "methodology"
			case "negative":
				kind = "template"
			default:
				kind = "poc"
			}
		}
		cat := e.VulnClass
		if cat == "" {
			cat = e.Category
		}
		cat = NormalizeVulnClass(cat, "info-leak")
		now := time.Now().UTC().Format(time.RFC3339)
		res, err := c.db.Exec("UPDATE poc SET title=?,kind=?,category=?,cve=?,component=?,description=?,content=?,updated_at=? WHERE code=? OR CAST(id AS TEXT)=?",
			e.Title, kind, cat, e.CVE, e.Product, e.Description, e.Content, now, e.ID, e.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("条目不存在")
		}
		idx := *e
		idx.ID = e.ID
		idx.Category = cat
		c.indexPocFTS(&idx)
		return nil
	}
	res, err := c.db.Exec("UPDATE entries SET title=?,category=?,vuln_class=?,product=?,content=? WHERE id=?",
		e.Title, e.Category, e.VulnClass, e.Product, e.Content, e.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("条目不存在")
	}
	return nil
}

// Delete 删除条目（含 FTS 索引与相对路径正文文件）。
func (c *Client) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id 必填")
	}
	if c.hasPoc {
		var rowid int64
		var pth sql.NullString
		err := c.db.QueryRow("SELECT id,path FROM poc WHERE code=? OR CAST(id AS TEXT)=? LIMIT 1", id, id).Scan(&rowid, &pth)
		if err == sql.ErrNoRows {
			return fmt.Errorf("条目不存在")
		}
		if err != nil {
			return err
		}
		if _, err := c.db.Exec("DELETE FROM poc_fts WHERE rowid=?", rowid); err != nil {
			return err
		}
		if _, err := c.db.Exec("DELETE FROM poc WHERE id=?", rowid); err != nil {
			return err
		}
		c.removeArtifact(pth.String)
		return nil
	}
	var file sql.NullString
	err := c.db.QueryRow("SELECT file FROM entries WHERE id=?", id).Scan(&file)
	if err == sql.ErrNoRows {
		return fmt.Errorf("条目不存在")
	}
	if err != nil {
		return err
	}
	if _, err := c.db.Exec("DELETE FROM entries WHERE id=?", id); err != nil {
		return err
	}
	c.removeArtifact(file.String)
	return nil
}

// removeArtifact 只删除知识库目录内解析成功的正文文件（防路径穿越）。
func (c *Client) removeArtifact(rel string) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return
	}
	full, err := c.resolvePath(rel)
	if err != nil {
		return
	}
	_ = os.Remove(full)
}

// Insights SQL 聚合统计。
func (c *Client) Insights() (*Insights, error) {
	out := &Insights{ByCategory: map[string]int{}, ByVulnClass: map[string]int{}, ByKind: map[string]int{}, Recent: []map[string]any{}}
	if c.hasPoc {
		if err := c.db.QueryRow("SELECT COUNT(*) FROM poc").Scan(&out.Total); err != nil {
			return nil, err
		}
		for _, bucket := range []string{"ammo", "tactic", "negative"} {
			var n int
			if err := c.db.QueryRow("SELECT COUNT(*) FROM poc WHERE " + bucketWhere(bucket)).Scan(&n); err == nil {
				out.ByCategory[bucket] = n
			}
		}
		rows, err := c.db.Query("SELECT COALESCE(category,''), COUNT(*) FROM poc GROUP BY category ORDER BY 2 DESC LIMIT 12")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var k string
				var n int
				if rows.Scan(&k, &n) == nil && k != "" {
					out.ByVulnClass[k] = n
				}
			}
		}
		krows, err := c.db.Query("SELECT COALESCE(kind,''), COUNT(*) FROM poc GROUP BY kind ORDER BY 2 DESC LIMIT 12")
		if err == nil {
			defer krows.Close()
			for krows.Next() {
				var k string
				var n int
				if krows.Scan(&k, &n) == nil && k != "" {
					out.ByKind[k] = n
				}
			}
		}
		rrows, err := c.db.Query("SELECT code,title,kind,category,description FROM poc ORDER BY id DESC LIMIT 10")
		if err != nil {
			return nil, err
		}
		defer rrows.Close()
		for rrows.Next() {
			var code, title, kind, cat, desc sql.NullString
			if rrows.Scan(&code, &title, &kind, &cat, &desc) == nil {
				out.Recent = append(out.Recent, map[string]any{
					"id": code.String, "title": title.String,
					"category":   classifyPocBucket(kind.String, cat.String, title.String, "", desc.String),
					"vuln_class": cat.String,
				})
			}
		}
		return out, rrows.Err()
	}
	if err := c.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := c.db.Query("SELECT COALESCE(category,''), COUNT(*) FROM entries GROUP BY category ORDER BY 2 DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil && k != "" {
			out.ByCategory[k] = n
		}
	}
	vrows, err := c.db.Query("SELECT COALESCE(vuln_class,''), COUNT(*) FROM entries GROUP BY vuln_class ORDER BY 2 DESC LIMIT 12")
	if err == nil {
		defer vrows.Close()
		for vrows.Next() {
			var k string
			var n int
			if vrows.Scan(&k, &n) == nil && k != "" {
				out.ByVulnClass[k] = n
			}
		}
	}
	rrows, err := c.db.Query("SELECT id,title,category,vuln_class FROM entries ORDER BY id DESC LIMIT 10")
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var id, title, cat string
		var vc sql.NullString
		if rrows.Scan(&id, &title, &cat, &vc) == nil {
			out.Recent = append(out.Recent, map[string]any{"id": id, "title": title, "category": cat, "vuln_class": vc.String})
		}
	}
	return out, rrows.Err()
}
