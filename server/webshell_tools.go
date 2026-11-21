package server

// WebShell agent 工具（M4-02）：generate_webshell。

import (
	"context"
	"encoding/json"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) webshellTools() []actool.CoreTool {
	return []actool.CoreTool{s.generateWebshellTool()}
}

func (s *Server) generateWebshellTool() actool.CoreTool {
	seed, _ := agent.WebshellToolMeta("generate_webshell")
	return actool.Build(actool.Spec{
		Name: "generate_webshell", Description: seed.Desc, Schema: seed.Schema,
		ReadOnly:   func(json.RawMessage) bool { return true }, // 只读：仅在平台侧渲染模板
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Lang      string `json:"lang"`
				Obfuscate bool   `json:"obfuscate"`
				Note      string `json:"note"`
			}
			_ = json.Unmarshal(in, &a)
			gen, err := session.GenerateShell(a.Lang, a.Obfuscate, a.Note)
			if err != nil {
				return actool.Errorf("马体生成失败：" + err.Error()), nil
			}
			b, _ := json.Marshal(gen)
			return actool.Text(string(b)), nil
		},
	})
}
