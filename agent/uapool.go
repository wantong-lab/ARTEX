package agent

// 客户端 UA 池（M1-05）：按任务稳定分配一个主流 UA，经 BashEnv 注入 ARTEX_UA。
// 动机：蜜罐常以「UA/TLS 指纹失配」触发；整任务固定一个 UA 才谈得上一致伪装，
// 半吊子混换比不装更糟。

import (
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"strings"
)

// DefaultUAPool 是 settings 键 web_ua_pool 未配置时的默认池。
func DefaultUAPool() []string {
	return []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	}
}

// UAPoolFromSetting 解析 settings 键 web_ua_pool（JSON 字符串数组）；
// 空/非法/无有效条目时回落默认池。
func UAPoolFromSetting(raw string) []string {
	var pool []string
	if err := json.Unmarshal([]byte(raw), &pool); err == nil {
		out := make([]string, 0, len(pool))
		for _, u := range pool {
			if u = strings.TrimSpace(u); u != "" {
				out = append(out, u)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return DefaultUAPool()
}

// PickUA 按任务稳定分配：hash(taskID) % len(pool)。同一任务永远同一 UA。
func PickUA(pool []string, taskID int64) string {
	if len(pool) == 0 {
		pool = DefaultUAPool()
	}
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(taskID))
	_, _ = h.Write(buf[:])
	return pool[h.Sum64()%uint64(len(pool))]
}
