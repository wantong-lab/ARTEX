package kb

// poc 表的 FTS 索引维护。
//
// 背景（2026-11-17）：kb.Add 原先在 hasPoc 分支里 insert 完 poc 表就 return，
// 从不写 poc_fts。后果是 agent 通过 kb_add 沉淀的知识「入库了但检索不到」——
// 实测 36,784 条里只有 3,603 条（9.8%）在 FTS 索引里，其余 90% 是死的。
//
// 这里补上写入：按 code 找到 rowid，先删旧索引行再插新行（幂等，支持 ON CONFLICT 更新）。

import (
	"database/sql"
	"log"
)

// indexPocFTS 把一条 poc 记录写入 poc_fts。失败只记日志，不影响入库本身
// （索引可以事后用 rebuild 脚本补齐，入库不能丢）。
func (c *Client) indexPocFTS(e *Entry) {
	if c == nil || c.db == nil || e == nil {
		return
	}

	var rowid int64
	if err := c.db.QueryRow(`SELECT id FROM poc WHERE code=?`, e.ID).Scan(&rowid); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[kb] FTS 索引跳过 %s: 取 rowid 失败 %v", e.ID, err)
		}
		return
	}

	// 幂等：先删同 rowid 的旧索引行（ON CONFLICT 更新场景会重复写）
	if _, err := c.db.Exec(`DELETE FROM poc_fts WHERE rowid=?`, rowid); err != nil {
		log.Printf("[kb] FTS 索引跳过 %s: 删旧行失败 %v", e.ID, err)
		return
	}

	content := e.Content
	if len(content) > 20000 {
		content = content[:20000] // 与重建脚本口径一致，避免索引膨胀
	}
	if _, err := c.db.Exec(`
INSERT INTO poc_fts(rowid, poc_id, title, cve, component, versions, tags, description, content)
VALUES (?,?,?,?,?,?,?,?,?)`,
		rowid, e.ID, e.Title, e.CVE, e.Product, "", e.Category, e.Description, content,
	); err != nil {
		log.Printf("[kb] FTS 索引失败 %s: %v", e.ID, err)
	}
}

// RebuildPocFTS 全量重建 poc_fts（供运维脚本/首次启用时调用）。
// 返回 (重建条数, error)。
func (c *Client) RebuildPocFTS() (int, error) {
	if _, err := c.db.Exec(`DELETE FROM poc_fts`); err != nil {
		return 0, err
	}
	rows, err := c.db.Query(`
SELECT id, code, title, cve, component, versions, tags, description, content
FROM poc`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type rec struct {
		id                           int64
		code, title, cve, comp, vers sql.NullString
		tags, desc, content          sql.NullString
	}
	var batch []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.code, &r.title, &r.cve, &r.comp,
			&r.vers, &r.tags, &r.desc, &r.content); err != nil {
			continue
		}
		batch = append(batch, r)
	}

	n := 0
	for _, r := range batch {
		content := r.content.String
		if len(content) > 20000 {
			content = content[:20000]
		}
		if _, err := c.db.Exec(`
INSERT INTO poc_fts(rowid, poc_id, title, cve, component, versions, tags, description, content)
VALUES (?,?,?,?,?,?,?,?,?)`,
			r.id, nz(r.code.String, ""), r.title.String, r.cve.String,
			r.comp.String, r.vers.String, r.tags.String, r.desc.String, content,
		); err == nil {
			n++
		}
	}
	return n, nil
}
