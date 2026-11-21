package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// nvdFixtureServer 返回一个分页 NVD 2.0 feed：page1=2 条、page2=1 条，共 3 条。
// 第二次请求（startIndex=2）会先通知测试再阻塞，供「中断后续跑」用例。
func nvdFixtureServer(t *testing.T, blockSecond bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
		total := 3
		mk := func(id, desc string) map[string]any {
			return map[string]any{"cve": map[string]any{
				"id": id, "published": "2024-01-02T00:00:00.000",
				"descriptions": []map[string]any{{"lang": "en", "value": desc}},
				"weaknesses":   []map[string]any{{"description": []map[string]any{{"value": "CWE-79"}}}},
				"references":   []map[string]any{{"url": "https://example.test/" + id, "tags": []string{"Exploit"}}},
			}}
		}
		var page struct {
			TotalResults    int              `json:"totalResults"`
			Vulnerabilities []map[string]any `json:"vulnerabilities"`
		}
		page.TotalResults = total
		switch start {
		case 0:
			page.Vulnerabilities = []map[string]any{mk("CVE-2024-0001", "alpha injection"), mk("CVE-2024-0002", "beta overflow")}
		default:
			if blockSecond {
				<-r.Context().Done()
				return
			}
			page.Vulnerabilities = []map[string]any{mk("CVE-2024-0003", "gamma traversal")}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
}

func newCVEFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cve.db"), nil, 0o600); err != nil {
		t.Fatalf("create cve.db: %v", err)
	}
	return dir
}

func TestCVESyncInterruptAndResume(t *testing.T) {
	dir := newCVEFixtureDir(t)
	srv := nvdFixtureServer(t, true)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var st CVESyncState
	var err error
	go func() {
		st, err = SyncCVEPaged(ctx, dir, CVESyncOptions{FeedURL: srv.URL, ResultsPerPage: 2})
		close(done)
	}()
	// 等第一页落库（断点文件出现 start_index=2），再中断。
	deadline := time.Now().Add(5 * time.Second)
	for {
		if LoadCVESyncState(dir).StartIndex == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first page was not checkpointed in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if err == nil {
		t.Fatal("interrupted sync should return ctx error")
	}
	if st.Done || st.StartIndex != 2 || st.Processed != 2 {
		t.Fatalf("checkpoint after interrupt: %+v", st)
	}
	// 断点已持久化
	if saved := LoadCVESyncState(dir); saved.StartIndex != 2 {
		t.Fatalf("saved checkpoint=%+v", saved)
	}

	// 续跑：换成可服务第二页的 feed，从断点继续到完成。
	plain := nvdFixtureServer(t, false)
	defer plain.Close()
	st2, err := SyncCVEPaged(context.Background(), dir, CVESyncOptions{FeedURL: plain.URL, ResultsPerPage: 2})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !st2.Done || st2.StartIndex != 3 || st2.Processed != 3 {
		t.Fatalf("after resume: %+v", st2)
	}

	// 幂等：再跑一遍不产生重复行
	c, err := OpenCVE(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	n1, _ := c.Count()
	_ = c.Close()
	if _, err := SyncCVEPaged(context.Background(), dir, CVESyncOptions{FeedURL: plain.URL, ResultsPerPage: 2}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	c, _ = OpenCVE(dir)
	n2, _ := c.Count()
	_ = c.Close()
	if n1 != n2 {
		t.Fatalf("resync should be idempotent: %d → %d", n1, n2)
	}
	// 基线种子 2 条 + 同步 3 条
	if n2 < 5 {
		t.Fatalf("expected >=5 rows, got %d", n2)
	}
}

var _ = fmt.Sprintf
