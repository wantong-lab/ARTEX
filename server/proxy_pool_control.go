package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) proxyPoolControlTool() actool.CoreTool {
	return wrTool("proxy_pool_control",
		"控制出口代理池：status 查看状态，auto_on/auto_off 开关封禁自动换 IP，rotate 立即切换下一个节点，pin 固定到指定节点，unpin 恢复自动选择。",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{"status", "auto_on", "auto_off", "rotate", "pin", "unpin"}, "description": "操作"},
				"node":   map[string]any{"type": "string", "description": "pin 时必填的节点名"},
			},
			"required": []string{"action"},
		},
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var req struct {
				Action string `json:"action"`
				Node   string `json:"node"`
			}
			_ = json.Unmarshal(in, &req)
			out, err := s.doProxyPoolControl(ctx, req.Action, req.Node)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text(out), nil
		})
}

func (s *Server) doProxyPoolControl(ctx context.Context, action, node string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "status", "":
		src, _ := s.egressSourceStore().GetSubscription(ctx)
		s.proxySubMu.Lock()
		st := s.proxySub
		s.proxySubMu.Unlock()
		if st == nil {
			if src == nil {
				return "代理池未配置", nil
			}
			return fmt.Sprintf("订阅代理未运行：desired=%s observed=%s error=%s", src.DesiredState, src.ObservedState, src.LastError), nil
		}
		return fmt.Sprintf("订阅代理运行中：http://127.0.0.1:%d\n当前节点：%s\n自动换 IP：%v\n封禁计数：%d\n出口模式：%s", st.port, st.current, st.autoRotate, st.banStrikes, s.m.EgressMode()), nil
	case "auto_on":
		if err := s.setProxyPoolAutoRotate(ctx, true); err != nil {
			return "", err
		}
		return "封禁自动换 IP：已启用", nil
	case "auto_off":
		if err := s.setProxyPoolAutoRotate(ctx, false); err != nil {
			return "", err
		}
		return "封禁自动换 IP：已关闭", nil
	case "rotate":
		return s.rotateProxyPoolNow(ctx)
	case "pin":
		node = strings.TrimSpace(node)
		if node == "" {
			return "", fmt.Errorf("pin 需要 node 参数")
		}
		if err := s.selectProxyPoolNode(ctx, "manual", node); err != nil {
			return "", err
		}
		return "已固定节点：" + node + "（自动换 IP 已关）", nil
	case "unpin":
		if err := s.setProxyPoolAutoRotate(ctx, true); err != nil {
			return "", err
		}
		return "已解除固定，恢复自动选择", nil
	default:
		return "", fmt.Errorf("未知 action: %q", action)
	}
}

func (s *Server) setProxyPoolAutoRotate(ctx context.Context, enabled bool) error {
	src, err := s.egressSourceStore().GetSubscription(ctx)
	if err != nil || src == nil {
		return fmt.Errorf("没有订阅源")
	}
	if err := s.egressSourceStore().UpdateAutoRotate(ctx, src.ID, enabled); err != nil {
		return err
	}
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	st := s.proxySub
	if st == nil {
		return fmt.Errorf("订阅代理未运行")
	}
	st.autoRotate = enabled
	if enabled {
		if err := s.applyMihomoSelection(ctx, st, "auto", ""); err != nil {
			return err
		}
		st.current = "ARTEX-AUTO"
	}
	return nil
}

func (s *Server) selectProxyPoolNode(ctx context.Context, mode, node string) error {
	src, err := s.egressSourceStore().GetSubscription(ctx)
	if err != nil || src == nil {
		return fmt.Errorf("没有订阅源")
	}
	if mode == "manual" {
		ok := false
		for _, n := range parseMihomoNodes(src.LastGoodConfig) {
			if n.Name == node {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("节点不存在")
		}
	}
	if err := s.egressSourceStore().UpdateSelection(ctx, src.ID, mode, node); err != nil {
		return err
	}
	if err := s.egressSourceStore().UpdateAutoRotate(ctx, src.ID, mode == "auto"); err != nil {
		return err
	}
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	st := s.proxySub
	if st == nil {
		return fmt.Errorf("订阅代理未运行")
	}
	if err := s.applyMihomoSelection(ctx, st, mode, node); err != nil {
		return err
	}
	st.autoRotate = mode == "auto"
	if mode == "manual" {
		st.current = node
	} else {
		st.current = "ARTEX-AUTO"
	}
	return nil
}

func (s *Server) rotateProxyPoolNow(ctx context.Context) (string, error) {
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	st := s.proxySub
	if st == nil || len(st.nodes) == 0 {
		return "", fmt.Errorf("订阅代理未运行")
	}
	cur := st.current
	next := ""
	for i, n := range st.nodes {
		if n.Name == cur {
			next = st.nodes[(i+1)%len(st.nodes)].Name
			break
		}
	}
	if next == "" {
		next = st.nodes[0].Name
	}
	if err := s.applyMihomoSelection(ctx, st, "manual", next); err != nil {
		return "", err
	}
	st.current = next
	return "已切换到节点：" + next, nil
}

// seedProxyPoolControlTool 把控制工具登记进 tools 表，默认绑定执行类 agent。
func (s *Server) seedProxyPoolControlTool() {
	if s.m == nil || s.m.pg == nil {
		return
	}
	desc := "控制出口代理池：status/auto_on/auto_off/rotate/pin/unpin。"
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"status", "auto_on", "auto_off", "rotate", "pin", "unpin"}},
			"node":   map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
	b, _ := json.Marshal(schema)
	agents, _ := json.Marshal([]string{"worker", "mainagent", "planner", "auto", "pentest"})
	_ = s.m.pg.SeedTool("proxy_pool_control", desc, b, agents)
}
