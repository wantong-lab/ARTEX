package kb

// 入库员：兼容原版 0.4.2 poc/poc_fts 表与 entries 表，查重 → 规范化 → 落库 → FTS 同步。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var fnameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeFilename(s string) string {
	return fnameRe.ReplaceAllString(strings.ToLower(s), "-")
}

func defaultFilename(e *Entry) string {
	base := e.Product
	if base == "" {
		base = e.Title
	}
	return sanitizeFilename(truncateRunes(base, 60)) + ".md"
}

func (e *Entry) validateAdd() error {
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("title 必填")
	}
	if e.Category == "" {
		e.Category = GuessCategory(e.Title + " " + e.Content)
	}
	return nil
}

// Add 落库：兼容 poc 表与 entries 表。
func (c *Client) Add(e *Entry) (string, error) {
	if err := e.validateAdd(); err != nil {
		return "", err
	}
	if e.ID == "" {
		e.ID = slugCode(e.Title)
		if e.ID == "" {
			e.ID = fmt.Sprintf("kb-%d", time.Now().Unix())
		}
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
		// 强制归一：模型常把 category 当标题写（中文长句/复合值/下划线/别名），
		// 直接落库会导致按类别检索漏掉、统计被拆碎。这里统一映射回标准类别。
		cat = NormalizeVulnClass(cat, "info-leak")
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := c.db.Exec(`INSERT INTO poc(code,title,kind,category,cve,component,description,content,path,verified,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,1,?,?)
			ON CONFLICT(code) DO UPDATE SET title=excluded.title,kind=excluded.kind,
			category=excluded.category,component=excluded.component,
			content=excluded.content,updated_at=excluded.updated_at`,
			e.ID, e.Title, kind, cat, e.CVE, e.Product, e.Description, e.Content, e.File, now, now)
		if err != nil {
			return "", err
		}
		// 关键：同步写 FTS 索引。原先这里直接 return，poc 表新条目从不进 poc_fts，
		// 导致 agent 沉淀的知识「入库了但搜不到」——实测 3.6 万条里只有 9.8% 可检索。
		c.indexPocFTS(e)
		return e.ID, nil
	}

	if len(e.Content) > 64<<10 {
		fn := filepath.Join("docs", defaultFilename(e))
		if err := os.MkdirAll(filepath.Join(c.dir, "pocs", "docs"), 0o755); err == nil {
			full := filepath.Join(c.dir, "pocs", fn)
			if werr := os.WriteFile(full, []byte(e.Content), 0o644); werr == nil {
				e.File, e.Content = fn, ""
			}
		}
	}
	_, err := c.db.Exec(`INSERT INTO entries(id,title,category,vuln_class,product,content,file)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title,category=excluded.category,
		vuln_class=excluded.vuln_class,product=excluded.product,
		content=excluded.content,file=excluded.file`,
		e.ID, e.Title, e.Category, e.VulnClass, e.Product, e.Content, e.File)
	if err != nil {
		return "", err
	}
	_ = c.reindexFTS()
	return e.ID, nil
}

func (c *Client) reindexFTS() error {
	if c.hasPoc {
		return nil
	}
	_, _ = c.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(id,title,content)`)
	_, _ = c.db.Exec(`INSERT INTO entries_fts(entries_fts) VALUES('rebuild')`)
	return nil
}

type MissingError struct{ ID string }

func (e *MissingError) Error() string { return "kb entry missing: " + e.ID }
