package kb

// Search：兼容原版 0.4.2 poc / poc_fts 表与 entries 表，分词 → 召回 → 打分排序 → 三档配比（弹药/打法/负面）。

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type Result struct {
	Total   int      `json:"total"`
	Entries []*Entry `json:"entries"`
}

func (r *Result) Total_() int { return r.Total }

// extractTerms 查询分词：空白切 + 小写 + 中英双语自动扩展，去噪词。
func extractTerms(q string) []string {
	var out []string
	seen := map[string]bool{}
	for _, sub := range ExpandBilingualQuery(q) {
		for _, w := range strings.Fields(strings.ToLower(sub)) {
			w = strings.TrimSpace(w)
			if len(w) >= 2 && !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}

// Search 全文检索。limit<=0 默认 20。
func (c *Client) Search(q string, limit int) (*Result, error) {
	if limit <= 0 {
		limit = 20
	}
	terms := extractTerms(q)
	if len(terms) == 0 {
		return c.query("", nil, limit)
	}
	// 优先走 FTS5（精确 AND 语义 + bm25 排序）。
	// 旧路径是 LIKE %term% 逐词 OR 全表扫描，长查询必然退化成「什么都命中」，
	// 实测表现为「查三个不同主题都返回同样 7 条」。
	if entries, ok := c.searchFTS(terms, limit); ok {
		if filtered := strictFilter(entries, terms); len(filtered) > 0 {
			if len(filtered) > limit {
				filtered = filtered[:limit]
			}
			return &Result{Total: len(filtered), Entries: filtered}, nil
		}
		// FTS 有结果但全被严格过滤掉 → 说明查询词太泛，宁可返回空，
		// 也不要拿一堆无关条目去污染调用方（漏洞上报的自动关联）。
		return &Result{Total: 0, Entries: nil}, nil
	}
	// FTS 不可用（老库/表缺失）才退到旧的 LIKE 路径。
	return c.query(strings.Join(terms, " OR "), terms, limit)
}

func (c *Client) query(_ string, terms []string, limit int) (*Result, error) {
	if c.hasPoc {
		return c.queryPoc(terms, limit)
	}
	var rows *sql.Rows
	var err error
	if len(terms) == 0 {
		rows, err = c.db.Query(`SELECT id,title,category,vuln_class,product,content,file FROM entries LIMIT ?`, limit*4)
	} else {
		var cond []string
		var args []any
		for _, t := range terms {
			cond = append(cond, `(title LIKE ? OR content LIKE ? OR product LIKE ?)`)
			p := "%" + escapeLike(t) + "%"
			args = append(args, p, p, p)
		}
		rows, err = c.db.Query(
			`SELECT id,title,category,vuln_class,product,content,file FROM entries WHERE `+
				strings.Join(cond, " OR ")+` LIMIT ?`,
			append(args, limit*4)...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []*Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Title, &e.Category, &e.VulnClass, &e.Product, &e.Content, &e.File); err == nil {
			e.Code = e.ID
			all = append(all, &e)
		}
	}
	return scoreAndFilter(all, terms, limit), nil
}

func (c *Client) queryPoc(terms []string, limit int) (*Result, error) {
	var rows *sql.Rows
	var err error
	fetchLim := limit * 5
	if fetchLim < 60 {
		fetchLim = 60
	}
	if len(terms) == 0 {
		rows, err = c.db.Query(`SELECT id,code,title,kind,category,cve,component,description,content,path,tags,verified,hit_count
			FROM poc ORDER BY hit_count DESC, id DESC LIMIT ?`, fetchLim)
	} else {
		var cond []string
		var args []any
		for _, t := range terms {
			cond = append(cond, `(title LIKE ? OR component LIKE ? OR cve LIKE ? OR tags LIKE ? OR description LIKE ? OR content LIKE ?)`)
			p := "%" + escapeLike(t) + "%"
			args = append(args, p, p, p, p, p, p)
		}
		rows, err = c.db.Query(
			`SELECT id,code,title,kind,category,cve,component,description,content,path,tags,verified,hit_count
			 FROM poc WHERE `+strings.Join(cond, " OR ")+` ORDER BY verified DESC, hit_count DESC, id DESC LIMIT ?`,
			append(args, fetchLim)...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []*Entry
	for rows.Next() {
		var (
			nid                                                         int64
			code, title, kind, cat, cve, comp, desc, content, pth, tags sql.NullString
			ver, hits                                                   sql.NullInt64
		)
		if err := rows.Scan(&nid, &code, &title, &kind, &cat, &cve, &comp, &desc, &content, &pth, &tags, &ver, &hits); err == nil {
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
				Category:    classifyPocBucket(kind.String, cat.String, title.String, tags.String, desc.String),
				VulnClass:   cat.String,
				Product:     nz(comp.String, cve.String),
				CVE:         cve.String,
				Description: desc.String,
				Content:     body,
				File:        pth.String,
				Verified:    ver.Int64 != 0,
				HitCount:    int(hits.Int64),
			}
			all = append(all, e)
		}
	}
	return scoreAndFilter(all, terms, limit), nil
}

// scoreEntry 标题命中 +10，product 命中 +6，弹药档 +3。
func scoreEntry(e *Entry, terms []string) int {
	s := 0
	t := strings.ToLower(e.Title + " " + e.Product + " " + e.CVE)
	body := strings.ToLower(e.Description + " " + e.Content)
	for _, w := range terms {
		if strings.Contains(t, w) {
			s += 10
		} else if strings.Contains(body, w) {
			s += 3
		}
	}
	if e.Category == "ammo" {
		s += 3
	}
	if e.Verified {
		s += 2
	}
	return s
}

func scoreAndFilter(all []*Entry, terms []string, limit int) *Result {
	for _, e := range all {
		e.Score = scoreEntry(e, terms)
		e.Content = truncateRunes(e.Content, 1600)
	}
	var ammo, tactic, neg []*Entry
	for _, e := range all {
		switch e.Category {
		case "ammo":
			ammo = append(ammo, e)
		case "negative":
			neg = append(neg, e)
		default:
			tactic = append(tactic, e)
		}
	}
	sortByScore(ammo)
	sortByScore(tactic)
	sortByScore(neg)
	var out []*Entry
	take := func(xs []*Entry, n int) {
		for i := 0; i < n && i < len(xs); i++ {
			out = append(out, xs[i])
		}
	}
	take(ammo, 3)
	take(tactic, 2)
	take(neg, 2)
	rest := append(append(append([]*Entry{}, ammo[min(3, len(ammo)):]...), tactic[min(2, len(tactic)):]...), neg[min(2, len(neg)):]...)
	sortByScore(rest)
	for _, e := range rest {
		if len(out) >= limit {
			break
		}
		out = append(out, e)
	}
	return &Result{Total: len(out), Entries: out}
}

func sortByScore(xs []*Entry) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].Score > xs[j-1].Score; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Brief 返回检索结果的紧凑 JSON（塞 prompt 用）。
func (c *Client) Brief(q string, limit int) string {
	res, err := c.Search(q, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	b, _ := json.Marshal(res)
	return string(b)
}

func ResultJSON(r *Result) string {
	b, _ := json.Marshal(r)
	return string(b)
}
