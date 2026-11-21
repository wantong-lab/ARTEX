package server

// C2 agent 工具（M4-01）：c2_list_sessions / c2_exec / c2_task_result。
// 默认关闭：c2_enabled != on 时工具一律返回明确错误，不会静默下发。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// 测试替换点：任务下发（默认走真实 c2.Manager）。
var c2Dispatch = func(rt *c2Runtime, ctx context.Context, uuid string, t *c2.Task, timeout time.Duration) (*c2.Result, error) {
	return rt.mgr.Dispatch(ctx, uuid, t, timeout)
}

var c2ToolMu sync.Mutex // 串行化工具内的「查库→下发→回写」

func (s *Server) c2Tools() []actool.CoreTool {
	return []actool.CoreTool{s.c2ListSessionsTool(), s.c2ExecTool(), s.c2TaskResultTool()}
}

func (s *Server) c2Gate() (*c2Runtime, error) {
	rt := s.c2rt()
	if rt == nil {
		return nil, fmt.Errorf("C2 运行时不可用（数据库未就绪）")
	}
	if !rt.enabled() {
		return nil, fmt.Errorf("C2 未启用：需管理员显式设置 c2_enabled=on 并配置监听器")
	}
	return rt, nil
}

func (s *Server) c2ListSessionsTool() actool.CoreTool {
	seed, _ := agent.C2ToolMeta("c2_list_sessions")
	return actool.Build(actool.Spec{
		Name: "c2_list_sessions", Description: seed.Desc, Schema: seed.Schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, _ json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			rt, err := s.c2Gate()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			sessions, err := rt.store.ListSessions(ctx, true)
			if err != nil {
				return actool.Errorf("会话列表读取失败：" + err.Error()), nil
			}
			if len(sessions) == 0 {
				return actool.Text(`{"sessions":[],"hint":"当前没有在线会话"}`), nil
			}
			b, _ := json.Marshal(map[string]any{"sessions": sessions})
			return actool.Text(string(b)), nil
		},
	})
}

func (s *Server) c2ExecTool() actool.CoreTool {
	seed, _ := agent.C2ToolMeta("c2_exec")
	return actool.Build(actool.Spec{
		Name: "c2_exec", Description: seed.Desc, Schema: seed.Schema,
		ReadOnly:   func(json.RawMessage) bool { return false },
		Concurrent: func(json.RawMessage) bool { return false },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				SessionID int64  `json:"session_id"`
				Command   string `json:"command"`
				Timeout   int    `json:"timeout_sec"`
			}
			_ = json.Unmarshal(in, &a)
			if a.SessionID <= 0 || strings.TrimSpace(a.Command) == "" {
				return actool.Errorf("session_id 与 command 为必填参数"), nil
			}
			rt, err := s.c2Gate()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			c2ToolMu.Lock()
			defer c2ToolMu.Unlock()
			sessions, err := rt.store.ListSessions(ctx, true)
			if err != nil {
				return actool.Errorf("会话查询失败：" + err.Error()), nil
			}
			var target *db.C2Session
			for _, sess := range sessions {
				if sess.ID == a.SessionID {
					target = sess
					break
				}
			}
			if target == nil {
				return actool.Errorf(fmt.Sprintf("会话 %d 不在线", a.SessionID)), nil
			}
			taskID, err := rt.store.CreateTask(ctx, target.ID, "exec", a.Command, "ai")
			if err != nil {
				return actool.Errorf("任务建单失败：" + err.Error()), nil
			}
			timeout := time.Duration(a.Timeout) * time.Second
			if timeout <= 0 || timeout > 300*time.Second {
				timeout = 120 * time.Second
			}
			started := time.Now()
			res, derr := c2Dispatch(rt, ctx, target.ImplantUUID, &c2.Task{Kind: "exec", Args: a.Command, Timeout: int(timeout.Seconds())}, timeout)
			if derr != nil {
				_ = rt.store.FinishTask(ctx, taskID, db.C2TaskFailed, "", derr.Error(), time.Since(started).Milliseconds())
				return actool.Text(fmt.Sprintf(`{"ok":false,"task_id":%d,"error":%q}`, taskID, derr.Error())), nil
			}
			out := c2.ClampText(res.Output, 16<<10)
			status := db.C2TaskSuccess
			if res.Err != "" || res.ExitCode != 0 {
				status = db.C2TaskFailed
			}
			_ = rt.store.FinishTask(ctx, taskID, status, out, res.Err, time.Since(started).Milliseconds())
			rt.store.AddEvent(ctx, "task", fmt.Sprintf("会话 %d 执行命令（%s）", target.ID, status), "info", target.ListenerID, target.ID, taskID)
			b, _ := json.Marshal(map[string]any{
				"ok": status == db.C2TaskSuccess, "task_id": taskID,
				"exit_code": res.ExitCode, "output": out, "error": res.Err,
			})
			return actool.Text(string(b)), nil
		},
	})
}

func (s *Server) c2TaskResultTool() actool.CoreTool {
	seed, _ := agent.C2ToolMeta("c2_task_result")
	return actool.Build(actool.Spec{
		Name: "c2_task_result", Description: seed.Desc, Schema: seed.Schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				TaskID int64 `json:"task_id"`
			}
			_ = json.Unmarshal(in, &a)
			rt, err := s.c2Gate()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if a.TaskID > 0 {
				t, err := rt.store.GetTask(ctx, a.TaskID)
				if err != nil {
					return actool.Errorf("任务读取失败：" + err.Error()), nil
				}
				if t == nil {
					return actool.Errorf(fmt.Sprintf("任务 %d 不存在", a.TaskID)), nil
				}
				b, _ := json.Marshal(t)
				return actool.Text(string(b)), nil
			}
			tasks, err := rt.store.ListTasks(ctx, 0, 20)
			if err != nil {
				return actool.Errorf("任务列表读取失败：" + err.Error()), nil
			}
			b, _ := json.Marshal(map[string]any{"tasks": tasks})
			return actool.Text(string(b)), nil
		},
	})
}
