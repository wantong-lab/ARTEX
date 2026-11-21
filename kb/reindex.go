package kb

// Reindex 重建当前库形态的全文索引：poc 库重建 poc_fts，entries 库重建 entries_fts。
// 返回索引覆盖的条目数（entries 库）；poc 库返回重建条数。
func (c *Client) Reindex() (int, error) {
	if c.hasPoc {
		return c.RebuildPocFTS()
	}
	if _, err := c.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(id,title,content)`); err != nil {
		return 0, err
	}
	if _, err := c.db.Exec(`INSERT INTO entries_fts(entries_fts) VALUES('rebuild')`); err != nil {
		return 0, err
	}
	var n int
	_ = c.db.QueryRow(`SELECT count(*) FROM entries`).Scan(&n)
	return n, nil
}
