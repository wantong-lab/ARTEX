package kb

// CVE 语料同步（M3-03）：NVD API 2.0 分页同步，断点持久化、ctx 取消可续跑。
// 断点写在 <dir>/cve.sync.json：每页 upsert 后落一次，进程被杀/人工中断后
// 下次从 startIndex 继续，不会从头重扫。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultNVDFeedURL = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	cveSyncStateFile  = "cve.sync.json"
	cveSyncMaxBody    = 64 << 20
)

// CVESyncState 是可持久化的同步断点。
type CVESyncState struct {
	FeedURL    string    `json:"feed_url,omitempty"`
	StartIndex int       `json:"start_index"`
	Processed  int       `json:"processed"`
	Total      int       `json:"total"`
	Done       bool      `json:"done"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

func cveSyncStatePath(dir string) string { return filepath.Join(dir, cveSyncStateFile) }

// LoadCVESyncState 读断点（无断点返回零值）。
func LoadCVESyncState(dir string) CVESyncState {
	var st CVESyncState
	b, err := os.ReadFile(cveSyncStatePath(dir))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

func saveCVESyncState(dir string, st CVESyncState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cveSyncStatePath(dir), b, 0o600)
}

// CVESyncOptions 控制同步窗口与节奏。
type CVESyncOptions struct {
	FeedURL        string        // 留空用 NVD 官方 API 2.0
	ResultsPerPage int           // 每页条数（默认 200）
	Interval       time.Duration // 页间停顿（默认 0；NVD 限流建议 6s 一次）
}

// NVD API 2.0 响应结构（只取同步需要的字段）。
type nvdAPIResponse struct {
	TotalResults    int `json:"totalResults"`
	Vulnerabilities []struct {
		Cve struct {
			ID           string `json:"id"`
			Published    string `json:"published"`
			Descriptions []struct {
				Lang  string `json:"lang"`
				Value string `json:"value"`
			} `json:"descriptions"`
			Weaknesses []struct {
				Description []struct {
					Value string `json:"value"`
				} `json:"description"`
			} `json:"weaknesses"`
			References []struct {
				URL  string   `json:"url"`
				Tags []string `json:"tags"`
			} `json:"references"`
		} `json:"cve"`
	} `json:"vulnerabilities"`
}

func nvdAPIToRecord(v *nvdAPIResponse, i int) *CVERecord {
	it := v.Vulnerabilities[i].Cve
	if strings.TrimSpace(it.ID) == "" {
		return nil
	}
	r := &CVERecord{ID: it.ID}
	if len(it.Published) >= 4 {
		fmt.Sscanf(it.Published[:4], "%d", &r.Year)
	}
	for _, d := range it.Descriptions {
		if d.Lang == "en" {
			r.Description = d.Value
		}
	}
	for _, w := range it.Weaknesses {
		for _, d := range w.Description {
			r.CWE = d.Value
		}
	}
	var refs []string
	for _, ref := range it.References {
		refs = append(refs, ref.URL)
		for _, t := range ref.Tags {
			if t == "Exploit" {
				r.HasExploit = true
			}
		}
	}
	r.Refs = strings.Join(refs, " ")
	return r
}

// upsertRecord 兼容两种库形态：原版 cve 表与 cves 表。
func (c *CVEClient) upsertRecord(r *CVERecord) error {
	if !c.hasCVE {
		return c.upsertCVE(r)
	}
	_, err := c.db.Exec(`INSERT INTO cve(id,year,vendor,products,published,desc,refs)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET year=excluded.year,desc=excluded.desc,refs=excluded.refs`,
		r.ID, r.Year, r.Vendor, r.Products, r.Published, r.Description, r.Refs)
	return err
}

// SyncCVEPaged 分页同步 NVD CVE：每页落库后保存断点；ctx 取消时返回已保存的状态与
// ctx.Err()（调用方据此续跑）。返回 Done=true 表示已同步到末尾。
func SyncCVEPaged(ctx context.Context, dir string, opts CVESyncOptions) (CVESyncState, error) {
	if strings.TrimSpace(dir) == "" {
		return CVESyncState{}, fmt.Errorf("dir 为空")
	}
	if opts.ResultsPerPage <= 0 {
		opts.ResultsPerPage = 200
	}
	if opts.ResultsPerPage > 2000 {
		opts.ResultsPerPage = 2000
	}
	if strings.TrimSpace(opts.FeedURL) == "" {
		opts.FeedURL = defaultNVDFeedURL
	}
	c, err := OpenCVE(dir)
	if err != nil {
		return CVESyncState{}, err
	}
	defer c.Close()
	st := LoadCVESyncState(dir)
	st.FeedURL = opts.FeedURL
	st.Done = false
	client := &http.Client{Timeout: 90 * time.Second}
	for {
		if err := ctx.Err(); err != nil {
			st.UpdatedAt = time.Now()
			_ = saveCVESyncState(dir, st)
			return st, err
		}
		u := fmt.Sprintf("%s?startIndex=%d&resultsPerPage=%d", opts.FeedURL, st.StartIndex, opts.ResultsPerPage)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return st, err
		}
		resp, err := client.Do(req)
		if err != nil {
			st.UpdatedAt = time.Now()
			_ = saveCVESyncState(dir, st)
			return st, err
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, cveSyncMaxBody))
		_ = resp.Body.Close()
		if rerr != nil {
			return st, rerr
		}
		if resp.StatusCode != http.StatusOK {
			return st, fmt.Errorf("NVD 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:minInt(len(body), 200)])))
		}
		var page nvdAPIResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return st, fmt.Errorf("解析 NVD 响应失败: %w", err)
		}
		if page.TotalResults > 0 {
			st.Total = page.TotalResults
		}
		if len(page.Vulnerabilities) == 0 {
			st.Done = true
			break
		}
		for i := range page.Vulnerabilities {
			if r := nvdAPIToRecord(&page, i); r != nil {
				if uerr := c.upsertRecord(r); uerr == nil {
					st.Processed++
				}
			}
		}
		st.StartIndex += len(page.Vulnerabilities)
		st.UpdatedAt = time.Now()
		if err := saveCVESyncState(dir, st); err != nil {
			return st, fmt.Errorf("保存同步断点失败: %w", err)
		}
		if st.Total > 0 && st.StartIndex >= st.Total {
			st.Done = true
			break
		}
		if opts.Interval > 0 {
			select {
			case <-time.After(opts.Interval):
			case <-ctx.Done():
				st.UpdatedAt = time.Now()
				_ = saveCVESyncState(dir, st)
				return st, ctx.Err()
			}
		}
	}
	st.UpdatedAt = time.Now()
	_ = saveCVESyncState(dir, st)
	return st, nil
}

// SyncCVE 保留旧入口：同步一页一次直到完成（不分页节奏），返回处理条数。
func SyncCVE(dir, feedURL string) (int, error) {
	st, err := SyncCVEPaged(context.Background(), dir, CVESyncOptions{FeedURL: feedURL})
	return st.Processed, err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// upsertCVE 写入兼容库（cves 表）；原版 cve 表走 upsertRecord。
func (c *CVEClient) upsertCVE(r *CVERecord) error {
	_, err := c.db.Exec(`INSERT INTO cves(id,year,description,cwe,severity,has_exploit,refs)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET description=excluded.description,
		cwe=excluded.cwe,severity=excluded.severity,
		has_exploit=excluded.has_exploit,refs=excluded.refs`,
		r.ID, r.Year, r.Description, r.CWE, r.Severity, b2i(r.HasExploit), r.Refs)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
