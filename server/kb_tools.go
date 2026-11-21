package server

// 知识库 agent 工具（M3-02）：kb_search / kb_add。
// handler 走 Server 的知识库句柄；不可用时返回明确错误而不是假装成功。

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/kb"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) kbTools() []actool.CoreTool {
	return []actool.CoreTool{s.kbSearchTool(), s.kbAddTool()}
}

func (s *Server) kbSearchTool() actool.CoreTool {
	return kbTool("kb_search", true, func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		var a struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		_ = json.Unmarshal(in, &a)
		q := strings.TrimSpace(a.Query)
		if q == "" {
			return actool.Errorf("query 为必填参数"), nil
		}
		c := s.kbClient()
		if c == nil {
			return actool.Errorf("知识库不可用"), nil
		}
		limit := a.Limit
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		res, err := c.Search(q, limit)
		if err != nil {
			return actool.Errorf("检索失败：" + err.Error()), nil
		}
		if res.Total == 0 {
			return actool.Text(`{"total":0,"entries":[],"hint":"知识库无相关条目；不要硬凑，按自己的判断继续"}`), nil
		}
		b, _ := json.Marshal(res)
		return actool.Text(string(b)), nil
	})
}

func (s *Server) kbAddTool() actool.CoreTool {
	return kbTool("kb_add", false, func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		var a struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Category  string `json:"category"`
			VulnClass string `json:"vuln_class"`
			Product   string `json:"product"`
			CVE       string `json:"cve"`
			Content   string `json:"content"`
		}
		_ = json.Unmarshal(in, &a)
		if strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Content) == "" {
			return actool.Errorf("title 与 content 为必填参数"), nil
		}
		c := s.kbClient()
		if c == nil {
			return actool.Errorf("知识库不可用"), nil
		}
		id, err := c.Add(&kb.Entry{
			ID: strings.TrimSpace(a.ID), Title: strings.TrimSpace(a.Title),
			Category: strings.TrimSpace(a.Category), VulnClass: strings.TrimSpace(a.VulnClass),
			Product: strings.TrimSpace(a.Product), CVE: strings.TrimSpace(a.CVE),
			Content: a.Content, Verified: true,
		})
		if err != nil {
			return actool.Errorf("入库失败：" + err.Error()), nil
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "id": id})
		return actool.Text(string(b)), nil
	})
}

func kbTool(name string, readOnly bool, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	seed, ok := agent.KBToolMeta(name)
	if !ok {
		return actool.Build(actool.Spec{Name: name, Description: name, Schema: map[string]any{"type": "object"}, Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		}})
	}
	return actool.Build(actool.Spec{
		Name: name, Description: seed.Desc, Schema: seed.Schema,
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
