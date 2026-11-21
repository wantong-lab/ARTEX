package server

// 会话操作内核（M4-02）：WebShell 管理 API 与 agent 工具共用同一套逻辑。
// 每次执行/读文件都会写 host_ledger；清理动作也登记到 host_ledger 供后续复核。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

// errShellNotFound 标记「会话不存在/已清理」，供 API 映射 404。
var errShellNotFound = errors.New("会话不存在")

// registerCore 探测并登记一个 WebShell 会话。
func (h *sessionHub) registerCore(ctx context.Context, rawURL, password, lang, secret string, assetID int64, artifactPath string) (map[string]any, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 为必填参数")
	}
	if strings.EqualFold(lang, "ssh") || strings.HasPrefix(rawURL, "ssh://") {
		return nil, fmt.Errorf("ssh 会话登记在后续里程碑提供；当前仅支持 HTTP webshell")
	}
	artifactPath = strings.TrimSpace(artifactPath)
	if artifactPath != "" {
		if strings.ContainsAny(artifactPath, "'\n\r\x00") {
			return nil, fmt.Errorf("目标路径含非法字符（引号/换行/NUL）")
		}
		if !strings.HasPrefix(artifactPath, "/") && !(len(artifactPath) > 2 && artifactPath[1] == ':' && (artifactPath[2] == '\\' || artifactPath[2] == '/')) {
			return nil, fmt.Errorf("目标路径需为绝对路径")
		}
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
		Kind: hs.Kind(), HostAssetID: assetID, URL: rawURL, ArtifactPath: artifactPath,
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

// cleanupShell 下线并清理 WebShell：配置了目标路径时先物理删除并校验，再删会话记录。
func (h *sessionHub) cleanupShell(ctx context.Context, id int64) (map[string]any, error) {
	rec, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("%w：会话 %d（可能已清理）", errShellNotFound, id)
	}
	host := h.hostOf(id, rec.URL)
	path := strings.TrimSpace(rec.ArtifactPath)
	res := map[string]any{"session_id": id, "host": host, "artifact_path": path, "physical": false, "verified": false}
	msg := "删除已上传 WebShell: " + host
	cleanupCmd := "cleanup webshell"
	cleanupOK := true
	note := "WebShell 管理页清理"

	if path != "" {
		sh, lerr := h.load(ctx, id)
		if lerr != nil {
			if h.ledger != nil {
				_ = h.ledger.AddHostOp(&db.HostOp{SessionID: id, Host: host, Channel: "webshell",
					Cmd: "rm " + path, OK: false, Artifact: true, AtType: "file",
					Cleanup: "物理清理失败: " + path, Note: lerr.Error()})
			}
			return nil, fmt.Errorf("物理清理失败：无法加载会话：%w", lerr)
		}
		rmCmd := "rm -f -- '" + path + "'"
		start := time.Now()
		stdout, stderr, xerr := sh.Exec(ctx, rmCmd, 30*time.Second)
		out := strings.TrimSpace(stdout + " " + stderr)
		ms := int(time.Since(start).Milliseconds())
		res["delete_command"] = rmCmd
		res["delete_output"] = truncateForNote(out, 300)
		cleanupCmd = rmCmd
		if xerr != nil {
			if h.ledger != nil {
				_ = h.ledger.AddHostOp(&db.HostOp{SessionID: id, Host: host, Channel: "webshell",
					Cmd: rmCmd, OK: false, Ms: ms, Artifact: true, AtType: "file",
					Cleanup: "物理删除失败: " + path, Note: truncateForNote(out, 200)})
			}
			return nil, fmt.Errorf("物理删除失败：%v（输出 %s）", xerr, truncateForNote(out, 200))
		}
		verifyCmd := "test ! -e '" + path + "' && echo ARGEX-GONE || echo ARGEX-PRESENT"
		vout, _, verr := sh.Exec(ctx, verifyCmd, 15*time.Second)
		verified := verr == nil && strings.Contains(vout, "ARGEX-GONE")
		res["physical"] = true
		res["verified"] = verified
		res["verify_output"] = truncateForNote(strings.TrimSpace(vout), 200)
		if !verified {
			if h.ledger != nil {
				_ = h.ledger.AddHostOp(&db.HostOp{SessionID: id, Host: host, Channel: "webshell",
					Cmd: rmCmd, OK: false, Ms: ms, Artifact: true, AtType: "file",
					Cleanup: "物理删除后校验失败: " + path, Note: truncateForNote(strings.TrimSpace(vout), 200)})
			}
			return nil, fmt.Errorf("删除后校验失败：目标路径仍存在（输出 %s）", truncateForNote(strings.TrimSpace(vout), 200))
		}
		msg = "已物理删除并校验: " + path + "（" + host + "）"
		note = "WebShell 管理页物理清理"
	}

	if h.ledger != nil {
		_ = h.ledger.AddHostOp(&db.HostOp{
			SessionID: id, Host: host, Channel: "webshell",
			Cmd: cleanupCmd, OK: cleanupOK, Artifact: true, AtType: "file",
			Cleanup: msg, Note: note,
		})
	}
	if err := h.store.Delete(ctx, id); err != nil {
		return nil, err
	}
	h.reg.Remove(id)
	h.mu.Lock()
	delete(h.host, id)
	h.mu.Unlock()
	res["cleanup"] = msg
	return res, nil
}

// truncateForNote 截断台账备注，避免 shell 输出过长。
func truncateForNote(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// shellHost 从会话 URL 取 host（用于展示）。
func shellHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// registerSSHCore 用密码/私钥直接登录 SSH，登记为可恢复的 ssh 会话。
func (h *sessionHub) registerSSHCore(ctx context.Context, sec session.SSHSecret, assetID int64) (map[string]any, error) {
	sec.Host = strings.TrimSpace(sec.Host)
	sec.User = strings.TrimSpace(sec.User)
	if sec.Host == "" || sec.User == "" {
		return nil, fmt.Errorf("host/user 必填")
	}
	if sec.Port <= 0 {
		sec.Port = 22
	}
	addr := net.JoinHostPort(sec.Host, strconv.Itoa(sec.Port))
	if h.gate != nil {
		if err := h.gate(ctx, "ssh://"+addr); err != nil {
			return nil, err
		}
	}
	sh, err := session.DialSSH(sec, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败：%w", err)
	}
	secret, err := json.Marshal(sh.Secret())
	if err != nil {
		return nil, fmt.Errorf("秘密序列化失败：%w", err)
	}
	ri := agent.RunInfoFrom(ctx)
	rec := &db.SessionRecord{
		Kind: "ssh", HostAssetID: assetID, URL: "ssh://" + sec.User + "@" + addr,
		Secret: secret, Lang: "ssh", Status: db.SessionAlive,
		CreatedByTask: ri.TaskID, CreatedByIntent: ri.IntentID,
	}
	id, err := h.store.Create(ctx, rec)
	if err != nil {
		_ = sh.Close(ctx)
		return nil, fmt.Errorf("登记失败：%w", err)
	}
	sh.SetID(id)
	h.reg.Add(sh)
	h.setHost(id, sec.Host)
	h.store.Touch(ctx, id)
	return map[string]any{"session_id": id, "kind": "ssh", "host": sec.Host,
		"port": sec.Port, "user": sec.User, "platform": sec.Platform}, nil
}

// registerSMBCore 用密码/NT hash 登记 Windows SMB（wmiexec）会话；每条命令独立往返。
func (h *sessionHub) registerSMBCore(ctx context.Context, sec session.SMBSecret, assetID int64) (map[string]any, error) {
	sec.Host = strings.TrimSpace(sec.Host)
	sec.User = strings.TrimSpace(sec.User)
	if sec.Host == "" || sec.User == "" {
		return nil, fmt.Errorf("host/user 必填")
	}
	if strings.TrimSpace(sec.Pass) == "" && strings.TrimSpace(sec.Hash) == "" {
		return nil, fmt.Errorf("需要 password 或 NT hash 其一")
	}
	if h.gate != nil {
		if err := h.gate(ctx, "smb://"+sec.Host); err != nil {
			return nil, err
		}
	}
	sh, err := session.DialSMB(sec)
	if err != nil {
		return nil, fmt.Errorf("SMB 参数校验失败：%w", err)
	}
	secret, err := json.Marshal(sh.Secret())
	if err != nil {
		return nil, fmt.Errorf("秘密序列化失败：%w", err)
	}
	ri := agent.RunInfoFrom(ctx)
	rec := &db.SessionRecord{
		Kind: "smbexec", HostAssetID: assetID, URL: "smb://" + sec.User + "@" + sec.Host,
		Secret: secret, Lang: "smbexec", Status: db.SessionAlive,
		CreatedByTask: ri.TaskID, CreatedByIntent: ri.IntentID,
	}
	id, err := h.store.Create(ctx, rec)
	if err != nil {
		return nil, fmt.Errorf("登记失败：%w", err)
	}
	sh.SetID(id)
	h.reg.Add(sh)
	h.setHost(id, sec.Host)
	h.store.Touch(ctx, id)
	return map[string]any{"session_id": id, "kind": "smbexec", "host": sec.Host,
		"user": sec.User, "hash_used": sec.Hash != ""}, nil
}
