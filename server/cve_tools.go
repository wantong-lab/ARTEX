package server

// CVE agent 工具（M3-03）：cve_search。

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) cveTools() []actool.CoreTool { return []actool.CoreTool{s.cveSearchTool()} }

func (s *Server) cveSearchTool() actool.CoreTool {
	seed, _ := agent.CVEToolMeta("cve_search")
	return actool.Build(actool.Spec{
		Name: "cve_search", Description: seed.Desc, Schema: seed.Schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			q := strings.TrimSpace(a.Query)
			if q == "" {
				return actool.Errorf("query 为必填参数"), nil
			}
			c := s.cveClient()
			if c == nil {
				return actool.Errorf("CVE 语料库不可用（未部署 cve.db）"), nil
			}
			limit := a.Limit
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			recs, err := c.Search(q, limit)
			if err != nil {
				return actool.Errorf("检索失败：" + err.Error()), nil
			}
			if len(recs) == 0 {
				return actool.Text(`{"total":0,"records":[],"hint":"无匹配 CVE；指纹匹配不等于可利用，继续按实际验证推进"}`), nil
			}
			b, _ := json.Marshal(map[string]any{"total": len(recs), "records": recs})
			return actool.Text(string(b)), nil
		},
	})
}
