package kb

// PentAGI Sploitus 漏洞利用搜索引擎移植 + 中英双语技术通道破壁（Language Policy）:
// 1. SearchSploitus: 直接查询 https://sploitus.com/search，拉取 CVE / 组件的公开 Exploit 源码与工具链接。
// 2. ExpandBilingualQuery: 将中文红队渗透检索词（如“泛微”、“致远”、“文件上传”、“反序列化”）自动扩展为中英双语词项，
//    解决中文意图在英文 CVE 语料库(cve.db)与 Sploitus 上命中率为 0 的问题。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	sploitusAPIURL         = "https://sploitus.com/search"
	sploitusRequestTimeout = 20 * time.Second
	maxSploitusSourceSize  = 16 * 1024
)

// SploitusItem 表示一条来自 Sploitus 的公开 EXP 或安全工具记录。
type SploitusItem struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Type      string  `json:"type"`
	Href      string  `json:"href"`
	Download  string  `json:"download,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Published string  `json:"published,omitempty"`
	Source    string  `json:"source,omitempty"`
	Language  string  `json:"language,omitempty"`
}

type sploitusResp struct {
	Exploits      []SploitusItem `json:"exploits"`
	ExploitsTotal int            `json:"exploits_total"`
}

// zhEnTechDict 常见中文组件与漏洞术语到英文 Technical Channel 的映射表（PentAGI Language Policy 内核落盘）。
var zhEnTechDict = []struct {
	zh string
	en string
}{
	{"泛微", "weaver e-cology e-office"},
	{"致远", "seeyon oa"},
	{"用友", "yonyou nc u8 grp"},
	{"蓝凌", "landray ekp"},
	{"通达", "tongda oa"},
	{"金蝶", "kingdee"},
	{"若依", "ruoyi"},
	{"宝塔", "bt panel baota"},
	{"禅道", "zentao"},
	{"海康", "hikvision"},
	{"大华", "dahua"},
	{"深信服", "sangfor"},
	{"天融信", "topsec"},
	{"奇安信", "qianxin"},
	{"绿盟", "nsfocus"},
	{"启明星辰", "venustech"},
	{"帆软", "finereport"},
	{"向日葵", "sunlogin"},
	{"文件上传", "arbitrary file upload"},
	{"任意文件读取", "arbitrary file read path traversal"},
	{"目录遍历", "directory traversal"},
	{"反序列化", "deserialization"},
	{"命令执行", "remote code execution command injection rce"},
	{"代码执行", "remote code execution rce"},
	{"sql注入", "sql injection sqli"},
	{"注入", "injection"},
	{"模板注入", "ssti template injection"},
	{"未授权", "unauthenticated authentication bypass"},
	{"越权", "idor privilege escalation"},
	{"提权", "privilege escalation privesc"},
	{"请求伪造", "ssrf"},
	{"实体注入", "xxe"},
}

// ExpandBilingualQuery 将含中文的查询词扩展出英文技术关键词，供英文 CVE 库与 Sploitus 检索。
func ExpandBilingualQuery(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	out := []string{q}
	lower := strings.ToLower(q)
	var enParts []string
	// 提取已有的 ASCII token（如 CVE-2024-xxxx, spring, shiro, fastjson 等）
	for _, tok := range strings.Fields(q) {
		isASCII := true
		for _, r := range tok {
			if r > 127 {
				isASCII = false
				break
			}
		}
		if isASCII && len(tok) >= 2 {
			enParts = append(enParts, tok)
		}
	}
	for _, m := range zhEnTechDict {
		if strings.Contains(lower, m.zh) {
			enParts = append(enParts, m.en)
		}
	}
	if len(enParts) > 0 {
		enQuery := strings.Join(enParts, " ")
		if !strings.EqualFold(enQuery, q) {
			out = append(out, enQuery)
		}
	}
	return out
}

// SearchSploitus 调用 Sploitus 搜索引擎获取公开 Exploit 源码与 PoC。
func SearchSploitus(ctx context.Context, query, exploitType string, limit int) ([]SploitusItem, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, fmt.Errorf("query is empty")
	}
	// 若查询含非 ASCII 中文字符，优先转换为英文 Technical Channel 查询词
	queries := ExpandBilingualQuery(query)
	techQuery := queries[len(queries)-1]

	if exploitType == "" {
		exploitType = "exploits"
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	reqBody := map[string]any{
		"query":  techQuery,
		"type":   exploitType,
		"sort":   "default",
		"title":  false,
		"offset": 0,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, sploitusRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, sploitusAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, err
	}
	referer := fmt.Sprintf("https://sploitus.com/?query=%s", url.QueryEscape(techQuery))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://sploitus.com")
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36")

	cli := sploitusClient()
	resp, err := cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("sploitus returned HTTP %d", resp.StatusCode)
	}
	var sr sploitusResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, 0, err
	}
	items := sr.Exploits
	if len(items) > limit {
		items = items[:limit]
	}
	for i := range items {
		if len(items[i].Source) > maxSploitusSourceSize {
			items[i].Source = items[i].Source[:maxSploitusSourceSize] + "\n... [truncated]"
		}
	}
	return items, sr.ExploitsTotal, nil
}

// sploitusClientVal 允许上层注入带代理的 http.Client（M4-03 代理池联动）。
var sploitusClientVal atomic.Value // *http.Client

func init() {
	sploitusClientVal.Store(&http.Client{Timeout: sploitusRequestTimeout})
}

// SetSploitusHTTPClient 注入出站客户端（nil 表示恢复默认）。
func SetSploitusHTTPClient(c *http.Client) {
	if c == nil {
		c = &http.Client{Timeout: sploitusRequestTimeout}
	}
	sploitusClientVal.Store(c)
}

func sploitusClient() *http.Client {
	return sploitusClientVal.Load().(*http.Client)
}
