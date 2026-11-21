package db

import "strconv"

// itoa64 是 int64 → string 的轻量助手（台账查询拼 SQL 用）。
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
