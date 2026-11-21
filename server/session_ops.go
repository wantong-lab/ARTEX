package server

// 会话操作内核（M4-02）：WebShell 管理 API 与 agent 工具共用同一套逻辑。
// 每次执行/读文件都会写 host_ledger；清理动作也登记到 host_ledger 供后续复核。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

// registerCore 探测并登记一个 WebShell 会话。
func (h *sessionHub) registerCore(ctx context.Context, rawURL, password, lang, secret string, assetID int64) (map[string]any, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 为必填参数")
	}
	if strings.EqualFold(lang, "ssh") || strings.HasPrefix(rawURL, "ssh://") {
		return nil, fmt.Errorf("ssh 会话登记在后续里程碑提供；当前仅支持 HTTP webshell")
	}
	if h.gate != nil {
		if err := h.gate(ctx, rawURL); err != nil {
			return nil, err
		}
	}
	sh, err := h.dial(ctx, rawURL, password, lang, secret)
	if err != nil {
		return nil, fmt.Errorf("探测失败：%w", err)
	}
	hs, ok := sh.(*session.HTTPShell)
	if !ok {
		return nil, fmt.Errorf("会话实现不受支持（需要 HTTP webshell）")
	}
	sec, err := json.Marshal(hs.Secret())
	if err != nil {
		return nil, fmt.Errorf("秘密序列化失败：%w", err)
	}
	ri := agent.RunInfoFrom(ctx)
	rec := &db.SessionRecord{
		Kind: hs.Kind(), HostAssetID: assetID, URL: rawURL,
		Secret: sec, Lang: hs.Lang(), Status: db.SessionAlive,
		CreatedByTask: ri.TaskID, CreatedByIntent: ri.IntentID,
	}
	id, err := h.store.Create(ctx, rec)
	if err != nil {
		return nil, fmt.Errorf("登记失败：%w", err)
	}
	hs.SetID(id)
	h.reg.Add(hs)
	h.setHost(id, rawURL)
	h.store.Touch(ctx, id)
	info := hs.Secret()
	return map[string]any{
		"session_id": id, "kind": hs.Kind(), "lang": hs.Lang(),
		"funcs": info.Funcs, "platform": info.Platform, "variant": info.Variant,
	}, nil
}

// execCore 在会话目标侧执行命令（与 session_exec 工具同一实现）。
func (h *sessionHub) execCore(ctx context.Context, id int64, command string, timeout time.Duration) (map[string]any, error) {
	if id <= 0 || strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("session_id 与 command 为必填参数")
	}
	sh, err := h.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if timeout > 600*time.Second {
		timeout = 600 * time.Second
	}
	start := time.Now()
	stdout, stderr, err := sh.Exec(ctx, command, timeout)
	ms := int(time.Since(start).Milliseconds())
	ok := err == nil
	body, truncated := truncateSessionOutput(stdout, sessionExecOutputCap)
	h.recordLedger(ctx, id, "", command, body, ok, ms, false, "", "")
	if err != nil {
		return nil, fmt.Errorf("执行失败（耗时 %dms）：%w", ms, err)
	}
	return map[string]any{"stdout": body, "stderr": stderr, "truncated": truncated, "ms": ms}, nil
}

// readCore 读目标侧文件（与 session_read 工具同一实现）。
func (h *sessionHub) readCore(ctx context.Context, id int64, path string, maxBytes int) (map[string]any, error) {
	if id <= 0 || strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("session_id 与 path 为必填参数")
	}
	sh, err := h.load(ctx, id)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	data, err := sh.ReadFile(ctx, path)
	ms := int(time.Since(start).Milliseconds())
	limit := maxBytes
	if limit <= 0 || limit > sessionReadCap {
		limit = 512 * 1024
	}
	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}
	h.recordLedger(ctx, id, "", "read "+path, "", err == nil, ms, false, "", "")
	if err != nil {
		return nil, fmt.Errorf("读取失败：%w", err)
	}
	res := map[string]any{"size": len(data), "truncated": truncated, "ms": ms}
	res["text"] = string(data) // WebShell 管理页按文本展示；二进制内容由 agent 工具走 base64 路径
	return res, nil
}

// listShells 列出 WebShell 类型的会话（http_*）。
func (h *sessionHub) listShells(ctx context.Context) ([]*db.SessionRecord, error) {
	all, err := h.store.List(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]*db.SessionRecord, 0, len(all))
	for _, rec := range all {
		if strings.HasPrefix(rec.Kind, "http_") {
			out = append(out, rec)
		}
	}
	return out, nil
}

// cleanupShell 下线并清理一个 WebShell：先登记 host_ledger 清理项，再删除会话记录。
func (h *sessionHub) cleanupShell(ctx context.Context, id int64) (string, error) {
	rec, err := h.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", fmt.Errorf("会话 %d 不存在（可能已清理）", id)
	}
	host := h.hostOf(id, rec.URL)
	cleanup := "删除已上传 WebShell: " + host
	if h.ledger != nil {
		_ = h.ledger.AddHostOp(&db.HostOp{
			SessionID: id, Host: host, Channel: "webshell",
			Cmd: "cleanup webshell", OK: true, Artifact: true, AtType: "file",
			Cleanup: cleanup, Note: "WebShell 管理页清理",
		})
	}
	if err := h.store.Delete(ctx, id); err != nil {
		return "", err
	}
	h.reg.Remove(id) // 内存注册表同步移除，清理后不可再执行
	h.mu.Lock()
	delete(h.host, id)
	h.mu.Unlock()
	return cleanup, nil
}

// shellHost 从会话 URL 取 host（用于展示）。
func shellHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
