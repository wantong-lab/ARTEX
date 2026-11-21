package server

// 知识入库员（M3-02）：漏洞确认后自动提炼一条打法入库，幂等（finding-<id> 为 ID）。
// 全部 best-effort：知识库不可用、写入失败只记日志，绝不影响漏洞上报。

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/kb"
)

// curatingRecorder 在漏洞记录成功后把打法沉淀进知识库。
type curatingRecorder struct {
	srv   *Server
	inner agent.FindingRecorder
}

func (c *curatingRecorder) Record(ctx context.Context, in db.RecordFindingInput, refs []db.TrafficRef) (*db.RecordedFinding, error) {
	out, err := c.inner.Record(ctx, in, refs)
	if err != nil || out == nil {
		return out, err
	}
	if k := c.srv.kbClient(); k != nil {
		if ierr := ingestFinding(k, in, out); ierr != nil {
			log.Printf("[kb] 漏洞 %d 知识入库失败（不影响漏洞记录）: %v", out.FindingID, ierr)
		}
	}
	return out, err
}

// findingRecorder 返回带知识入库装饰的 FindingRecorder（agent 侧注入用）。
func (s *Server) findingRecorder() agent.FindingRecorder {
	return &curatingRecorder{srv: s, inner: s.evidenceStore()}
}

// ingestFinding 把一条已确认漏洞提炼成知识条目（幂等：ID 由 finding id 决定）。
func ingestFinding(k *kb.Client, in db.RecordFindingInput, out *db.RecordedFinding) error {
	if k == nil || out == nil {
		return nil
	}
	title := strings.TrimSpace(in.Name)
	if title == "" {
		title = strings.TrimSpace(in.Summary)
	}
	if title == "" {
		return nil
	}
	body := strings.TrimSpace(in.Summary)
	if ev := strings.TrimSpace(in.Evidence); ev != "" {
		body += "\n\n证据：\n" + ev
	}
	body = anonymizeContent(body)
	_, err := k.Add(&kb.Entry{
		ID:        fmt.Sprintf("finding-%d", out.FindingID),
		Title:     anonymizeContent(title),
		Category:  "tactic",
		VulnClass: in.VulnClass,
		Content:   body,
		Verified:  true,
	})
	return err
}
