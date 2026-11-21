package server

// 会话工具（M2-02）：把拿到的 webshell 登记为平台管理会话，并提供 exec/read/write。
// 秘密经 db.SessionStore 的 gcm1 加密落库；每次执行写 host_ledger 台账；
// 登记前过资产拦截（RoE）门；工具调用本身走 guard/intercept 审批链。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const (
	sessionExecOutputCap = 16 * 1024
	sessionReadCap       = 1 << 20
	sessionWriteCap      = 1 << 20
	sessionStaleAfter    = 10 * time.Minute
)

// sessionStoreAPI 是 sessions 表的最小访问面（测试注入内存实现）。
type sessionStoreAPI interface {
	Create(ctx context.Context, rec *db.SessionRecord) (int64, error)
	Get(ctx context.Context, id int64) (*db.SessionRecord, error)
	UpdateStatus(ctx context.Context, id int64, status string) error
	Touch(ctx context.Context, id int64)
	List(ctx context.Context, status string) ([]*db.SessionRecord, error)
	Delete(ctx context.Context, id int64) error
}

// hostLedgerAPI 是主机台账写入面。
type hostLedgerAPI interface {
	AddHostOp(o *db.HostOp) error
}

// sessionHub 是进程级会话中枢：注册表 + 落库 + 台账 + RoE 门。
type sessionHub struct {
	store  sessionStoreAPI
	ledger hostLedgerAPI
	gate   func(ctx context.Context, rawURL string) error
	reg    *session.Registry
	dial   func(ctx context.Context, rawURL, password, lang, secret string) (session.Session, error)
	now    func() time.Time

	mu   sync.Mutex
	host map[int64]string // session_id → 目标 host（台账用）
}

func newSessionHub(store sessionStoreAPI, ledger hostLedgerAPI, gate func(context.Context, string) error) *sessionHub {
	h := &sessionHub{
		store: store, ledger: ledger, gate: gate,
		reg: session.NewRegistry(), now: time.Now, host: map[int64]string{},
	}
	h.dial = func(ctx context.Context, rawURL, password, lang, secret string) (session.Session, error) {
		switch strings.ToLower(strings.TrimSpace(lang)) {
		case "phpenc", "jspenc", "aspxenc":
			sh, _, err := session.ProbeEnc(ctx, rawURL, lang, secret)
			if err != nil {
				return nil, err
			}
			return sh, nil
		default:
			sh, _, err := session.Probe(ctx, rawURL, password, lang)
			if err != nil {
				return nil, err
			}
			return sh, nil
		}
	}
	return h
}

func (h *sessionHub) setHost(id int64, rawURL string) {
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Hostname()
	}
	h.mu.Lock()
	h.host[id] = host
	h.mu.Unlock()
}

func (h *sessionHub) hostOf(id int64, rawURL string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if v, ok := h.host[id]; ok {
		return v
	}
	if u, err := url.Parse(rawURL); err == nil {
		return u.Hostname()
	}
	return ""
}

// load 取会话：先查进程注册表，未命中从库里解密恢复，last_beat 超时时做启动探活。
func (h *sessionHub) load(ctx context.Context, id int64) (session.Session, error) {
	if sh, ok := h.reg.Get(id); ok {
		return sh, nil
	}
	rec, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("会话 %d 读取失败: %w", id, err)
	}
	if rec.Status != db.SessionAlive {
		return nil, fmt.Errorf("会话 %d 状态为 %s，不可执行；请重新登记", id, rec.Status)
	}
	var sec session.Secret
	if err := json.Unmarshal(rec.Secret, &sec); err != nil {
		return nil, fmt.Errorf("会话 %d 秘密数据不可解析: %w", id, err)
	}
	sh, err := session.RestoreHTTPShell(rec.ID, sec)
	if err != nil {
		return nil, fmt.Errorf("会话 %d 恢复失败（仅支持 HTTP 会话）: %w", id, err)
	}
	h.reg.Add(sh)
	h.setHost(id, rec.URL)
	if !rec.LastBeat.IsZero() && h.now().Sub(rec.LastBeat) > sessionStaleAfter {
		tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := sh.Test(tctx); err != nil {
			_ = h.store.UpdateStatus(ctx, id, db.SessionDead)
			h.reg.Remove(id)
			return nil, fmt.Errorf("会话 %d 探活失败，已标记 dead: %w", id, err)
		}
		h.store.Touch(ctx, id)
	}
	return sh, nil
}

func (h *sessionHub) recordLedger(ctx context.Context, sessionID int64, rawURL, cmd, stdout string, ok bool, ms int, artifact bool, atype, cleanup string) {
	if h.ledger == nil {
		return
	}
	_ = h.ledger.AddHostOp(&db.HostOp{
		SessionID: sessionID, Host: h.hostOf(sessionID, rawURL), Channel: "session",
		Cmd: cmd, Stdout: stdout, OK: ok, Ms: ms,
		Artifact: artifact, AtType: atype, Cleanup: cleanup,
	})
}

func (h *sessionHub) registerTool() actool.CoreTool {
	return sessionTool("register_session",
		"【立足点主通道】把已拿到的 webshell 登记为平台管理的会话。先发无害探针探测马型与可用执行函数，探测成功才落库；"+
			"失败返回明确原因（不伪造成功）。登记目标 host 会过资产拦截(RoE)校验。lang: php/phpcmd/jsp/aspx/auto（明文），phpenc/jspenc/aspxenc（加密马，需 secret）；ssh 暂不支持。",
		map[string]any{
			"url":      sessStr("webshell 完整 URL，含协议（必填）"),
			"password": sessStr("连接密码（POST 字段名，默认 cmd；加密马不需要）"),
			"lang":     sessStr("马语言：php / phpcmd / jsp / aspx / auto（默认 auto）；phpenc / jspenc / aspxenc；ssh 暂不支持"),
			"secret":   sessStr("加密马密钥（phpenc/jspenc/aspxenc 必填）"),
			"asset_id": sessInt("该会话对应资产 id（可选，用于把会话挂到已登记的服务资产）"),
		}, []string{"url"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				URL      string `json:"url"`
				Password string `json:"password"`
				Lang     string `json:"lang"`
				Secret   string `json:"secret"`
				AssetID  int64  `json:"asset_id"`
			}
			_ = json.Unmarshal(in, &a)
			a.URL = strings.TrimSpace(a.URL)
			if a.URL == "" {
				return actool.Errorf("url 为必填参数"), nil
			}
			if strings.EqualFold(a.Lang, "ssh") || strings.HasPrefix(a.URL, "ssh://") {
				return actool.Errorf("ssh 会话登记在后续里程碑提供；当前仅支持 HTTP webshell"), nil
			}
			if h.gate != nil {
				if err := h.gate(ctx, a.URL); err != nil {
					return actool.Errorf(err.Error()), nil
				}
			}
			sh, err := h.dial(ctx, a.URL, a.Password, a.Lang, a.Secret)
			if err != nil {
				return actool.Errorf("探测失败：" + err.Error()), nil
			}
			hs, ok := sh.(*session.HTTPShell)
			if !ok {
				return actool.Errorf("会话实现不受支持（需要 HTTP webshell）"), nil
			}
			sec, err := json.Marshal(hs.Secret())
			if err != nil {
				return actool.Errorf("秘密序列化失败：" + err.Error()), nil
			}
			ri := agent.RunInfoFrom(ctx)
			rec := &db.SessionRecord{
				Kind: hs.Kind(), HostAssetID: a.AssetID, URL: a.URL,
				Secret: sec, Lang: hs.Lang(), Status: db.SessionAlive,
				CreatedByTask: ri.TaskID, CreatedByIntent: ri.IntentID,
			}
			id, err := h.store.Create(ctx, rec)
			if err != nil {
				return actool.Errorf("登记失败：" + err.Error()), nil
			}
			hs.SetID(id)
			h.reg.Add(hs)
			h.setHost(id, a.URL)
			h.store.Touch(ctx, id)
			info := hs.Secret()
			out, _ := json.Marshal(map[string]any{
				"session_id": id, "kind": hs.Kind(), "lang": hs.Lang(),
				"funcs": info.Funcs, "platform": info.Platform, "variant": info.Variant,
			})
			return actool.Text(string(out)), nil
		})
}

func (h *sessionHub) execTool() actool.CoreTool {
	return sessionTool("session_exec",
		"在已登记会话的目标侧执行一条 shell 命令（webshell 通道 stderr 合并进 stdout）。输出上限 16KB，超出截断。每次执行自动写 host_ledger 台账。注意 OPSEC。",
		map[string]any{
			"session_id":      sessInt("会话 id（register_session 返回）"),
			"command":         sessStr("要执行的 shell 命令（必填）"),
			"timeout_seconds": sessInt("超时秒数（默认 60，最大 600）"),
		}, []string{"session_id", "command"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64  `json:"session_id"`
				Command   string `json:"command"`
				Timeout   int    `json:"timeout_seconds"`
			}
			_ = json.Unmarshal(in, &a)
			if a.SessionID <= 0 || strings.TrimSpace(a.Command) == "" {
				return actool.Errorf("session_id 与 command 为必填参数"), nil
			}
			sh, err := h.load(ctx, a.SessionID)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			timeout := time.Duration(a.Timeout) * time.Second
			if timeout <= 0 {
				timeout = 60 * time.Second
			}
			if timeout > 600*time.Second {
				timeout = 600 * time.Second
			}
			start := time.Now()
			stdout, stderr, err := sh.Exec(ctx, a.Command, timeout)
			ms := int(time.Since(start).Milliseconds())
			ok := err == nil
			body, truncated := truncateSessionOutput(stdout, sessionExecOutputCap)
			h.recordLedger(ctx, a.SessionID, "", a.Command, body, ok, ms, false, "", "")
			if err != nil {
				return actool.Errorf(fmt.Sprintf("执行失败（耗时 %dms）：%s", ms, err.Error())), nil
			}
			out, _ := json.Marshal(map[string]any{"stdout": body, "stderr": stderr, "truncated": truncated, "ms": ms})
			return actool.Text(string(out)), nil
		})
}

func (h *sessionHub) readTool() actool.CoreTool {
	return sessionTool("session_read",
		"读会话目标侧的文件（上限 1MB）。文本内容直接返回 text；二进制返回 base64。每次读自动写 host_ledger 台账。",
		map[string]any{
			"session_id": sessInt("会话 id"),
			"path":       sessStr("目标侧文件路径（必填）"),
			"max_bytes":  sessInt("最多读取字节数（默认 524288，最大 1048576）"),
		}, []string{"session_id", "path"}, true,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64  `json:"session_id"`
				Path      string `json:"path"`
				MaxBytes  int    `json:"max_bytes"`
			}
			_ = json.Unmarshal(in, &a)
			if a.SessionID <= 0 || strings.TrimSpace(a.Path) == "" {
				return actool.Errorf("session_id 与 path 为必填参数"), nil
			}
			sh, err := h.load(ctx, a.SessionID)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			start := time.Now()
			data, err := sh.ReadFile(ctx, a.Path)
			ms := int(time.Since(start).Milliseconds())
			limit := a.MaxBytes
			if limit <= 0 || limit > sessionReadCap {
				limit = 512 * 1024
			}
			truncated := false
			if len(data) > limit {
				data = data[:limit]
				truncated = true
			}
			h.recordLedger(ctx, a.SessionID, "", "read "+a.Path, "", err == nil, ms, false, "", "")
			if err != nil {
				return actool.Errorf("读取失败：" + err.Error()), nil
			}
			res := map[string]any{"size": len(data), "truncated": truncated, "ms": ms}
			if utf8.Valid(data) {
				res["text"] = string(data)
			} else {
				res["base64"] = base64.StdEncoding.EncodeToString(data)
			}
			out, _ := json.Marshal(res)
			return actool.Text(string(out)), nil
		})
}

func (h *sessionHub) writeTool() actool.CoreTool {
	return sessionTool("session_write",
		"写文件到会话目标侧（上限 1MB；content 为文本，二进制用 content_b64）。写文件属于持久化痕迹，会写 host_ledger(artifact=true, cleanup=rm -f)。注意 OPSEC 与清理。",
		map[string]any{
			"session_id":  sessInt("会话 id"),
			"path":        sessStr("目标侧文件路径（必填）"),
			"content":     sessStr("文本内容（与 content_b64 二选一）"),
			"content_b64": sessStr("base64 内容（二进制文件用）"),
			"append":      map[string]any{"type": "boolean", "description": "是否追加（默认覆盖）"},
		}, []string{"session_id", "path"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID  int64  `json:"session_id"`
				Path       string `json:"path"`
				Content    string `json:"content"`
				ContentB64 string `json:"content_b64"`
				Append     bool   `json:"append"`
			}
			_ = json.Unmarshal(in, &a)
			if a.SessionID <= 0 || strings.TrimSpace(a.Path) == "" {
				return actool.Errorf("session_id 与 path 为必填参数"), nil
			}
			data := []byte(a.Content)
			if a.ContentB64 != "" {
				dec, err := base64.StdEncoding.DecodeString(a.ContentB64)
				if err != nil {
					return actool.Errorf("content_b64 解码失败：" + err.Error()), nil
				}
				data = dec
			}
			if len(data) > sessionWriteCap {
				return actool.Errorf(fmt.Sprintf("内容超过 %d 字节上限", sessionWriteCap)), nil
			}
			if a.Append {
				if old, err := h.readForAppend(ctx, a.SessionID, a.Path); err == nil {
					data = append(old, data...)
				}
			}
			sh, err := h.load(ctx, a.SessionID)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			start := time.Now()
			err = sh.WriteFile(ctx, a.Path, data)
			ms := int(time.Since(start).Milliseconds())
			h.recordLedger(ctx, a.SessionID, "", "write "+a.Path, "", err == nil, ms, true, "file", "rm -f "+a.Path)
			if err != nil {
				return actool.Errorf("写入失败：" + err.Error()), nil
			}
			out, _ := json.Marshal(map[string]any{"ok": true, "bytes": len(data), "ms": ms, "path": a.Path})
			return actool.Text(string(out)), nil
		})
}

func (h *sessionHub) readForAppend(ctx context.Context, id int64, path string) ([]byte, error) {
	sh, err := h.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return sh.ReadFile(ctx, path)
}

// sessionToolsForTask 返回绑定任务的四个会话工具。
func (h *sessionHub) tools() []actool.CoreTool {
	return []actool.CoreTool{h.registerTool(), h.execTool(), h.readTool(), h.writeTool()}
}

func truncateSessionOutput(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	return s[:limit] + fmt.Sprintf("\n…[输出截断：原始 %d 字节，仅保留前 %d]", len(s), limit), true
}

func sessionTool(name, desc string, props map[string]any, required []string, readOnly bool, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i, v := range required {
			r[i] = v
		}
		schema["required"] = r
	}
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:   func(json.RawMessage) bool { return readOnly },
		Concurrent: func(json.RawMessage) bool { return readOnly },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

// sessions 返回进程级会话中枢（惰性初始化：秘密加密用 jwtKey 派生）。
func (s *Server) sessions() *sessionHub {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessHub != nil {
		return s.sessHub
	}
	store := db.NewSessionStore(s.m.pg, s.jwtKey)
	var gate func(context.Context, string) error
	if s.m.pg != nil && s.m.Assets() != nil {
		gate = func(ctx context.Context, rawURL string) error {
			taskID := agent.RunInfoFrom(ctx).TaskID
			rules, _ := s.m.Assets().ListAssetInterceptRules()
			var allow []db.AssetInterceptRule
			if taskID > 0 {
				if tb, ta, err := s.m.Assets().TaskInterceptRulesSplit(taskID); err == nil {
					rules = append(rules, tb...)
					allow = ta
				}
			}
			host := ""
			if u, err := url.Parse(rawURL); err == nil {
				host = u.Hostname()
			}
			var domains, ips []string
			if host != "" {
				if net.ParseIP(host) != nil {
					ips = []string{host}
				} else {
					domains = []string{host}
				}
			}
			if d := db.EvaluateAssetGate(rules, allow, domains, ips, []string{rawURL}); !d.Allowed {
				return fmt.Errorf("目标未通过测试范围校验：%s", d.Reason)
			}
			return nil
		}
	}
	s.sessHub = newSessionHub(store, s.m.pg, gate)
	return s.sessHub
}
func sessStr(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func sessInt(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
