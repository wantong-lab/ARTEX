package kb

// FTS5 优先的检索实现。
//
// 背景（2026-11-17 实测）：
//   - 原 queryPoc 用 `LIKE %term%` 逐词 OR 全表扫描，长查询被切成几十个词后
//     OR 一连接几乎任何条目都能命中一个词，导致「查什么都返回同样 7 条」；
//   - 同时把 content 全文塞进结果，单次返回 25KB，占 6K+ token；
//   - poc 表 36,784 条，但 poc_fts 只索引了 3,603 条（9.8%），90% 内容搜不到。
//
// 本实现改为：
//   1. 优先走 poc_fts（FTS5），用 AND 语义（bm25 排序），命中不了才退 LIKE；
//   2. 查询词做转义（FTS5 把 - : * 等当语法，裸用会报 "no such column"）；
//   3. 结果严格打分：只保留真正相关的（标题/组件命中优先），无关的不凑数；
//   4. Content 截断，避免 25KB 的返回。

import (
	"database/sql"
	"fmt"
	"strings"
)

// ftsEscapeToken 把一个词转成 FTS5 安全的形式：整体加双引号，内部双引号翻倍。
func ftsEscapeToken(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	return `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
}

// buildFTSQuery 用 AND 连接各词（全部命中才返回），比 OR 精准得多。
func buildFTSQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		if e := ftsEscapeToken(t); e != "" {
			parts = append(parts, e)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}

// searchFTS 走 FTS5 检索，返回条目（不含 content 全文，只给摘要）。
// 找不到表/索引时返回 (nil, false) 让调用方退到旧路径。
func (c *Client) searchFTS(terms []string, limit int) ([]*Entry, bool) {
	if len(terms) == 0 {
		return nil, false
	}
	q := buildFTSQuery(terms)
	if q == "" {
		return nil, false
	}
	// 先取比 limit 多的候选，供二次打分筛掉噪声。
	fetch := limit * 6
	if fetch < 40 {
		fetch = 40
	}
	rows, err := c.db.Query(`
SELECT p.id, p.code, p.title, p.kind, p.category, p.cve, p.component,
       p.description, p.tags, p.verified, p.hit_count,
       bm25(poc_fts) AS rank
FROM poc_fts
JOIN poc p ON p.id = poc_fts.rowid
WHERE poc_fts MATCH ?
ORDER BY rank
LIMIT ?`, q, fetch)
	if err != nil {
		return nil, false // FTS 表不存在或语法问题 → 退旧路径
	}
	defer rows.Close()

	var out []*Entry
	for rows.Next() {
		var (
			nid                         int64
			code, title, kind, cat, cve sql.NullString
			comp, desc, tags            sql.NullString
			ver, hits                   sql.NullInt64
			rank                        sql.NullFloat64
		)
		if err := rows.Scan(&nid, &code, &title, &kind, &cat, &cve, &comp,
			&desc, &tags, &ver, &hits, &rank); err != nil {
			continue
		}
		e := &Entry{
			ID:          nz(code.String, fmt.Sprint(nid)),
			Code:        nz(code.String, fmt.Sprint(nid)),
			Title:       title.String,
			Kind:        kind.String,
			Category:    classifyPocBucket(kind.String, cat.String, title.String, tags.String, desc.String),
			VulnClass:   cat.String,
			Product:     nz(comp.String, cve.String),
			CVE:         cve.String,
			Description: desc.String,
			Verified:    ver.Int64 != 0,
			HitCount:    int(hits.Int64),
			// Content 故意留空：FTS 路径只给摘要，正文按需用 code 再取。
			// 这样单条结果从 ~700B 降到 ~200B，10 条也就 2KB。
		}
		out = append(out, e)
	}
	return out, true
}

// strictFilter 只保留真正相关的条目：标题/组件/CVE 命中任一查询词才算。
// 这是「不要凑数」的关键——宁可返回 2 条真的，也不返回 7 条泛的。
func strictFilter(entries []*Entry, terms []string) []*Entry {
	if len(terms) == 0 {
		return entries
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		head := strings.ToLower(e.Title + " " + e.Product + " " + e.CVE + " " + e.VulnClass)
		for _, t := range terms {
			if strings.Contains(head, strings.ToLower(t)) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
