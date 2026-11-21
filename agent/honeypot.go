package agent

// 蜜罐静态签名评分挂点（M1-04）：服务资产落库后跑 honeydetect 签名评估，
// 命中即回写资产评分并写一条 fact 锚定（planner 经探索图可见）。
// 全部 best-effort：任何失败都只影响蜜罐标记，不影响资产登记主流程。

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/honeydetect"
)

// serviceViewFromInsert 把 insert_assets 的 service 项映射成签名评估输入。
// 只在登记时能拿到 title/technologies/port；没有响应体与证书，
// 因此依赖 body/cert 的签名此阶段不会命中（后续指纹回写可增强）。
func serviceViewFromInsert(it assetInputItem) honeydetect.ServiceView {
	v := honeydetect.ServiceView{
		Title:  strings.TrimSpace(it.PageTitle),
		Banner: strings.Join(it.Technologies, " "),
	}
	if it.URL != "" {
		if u, err := url.Parse(it.URL); err == nil {
			if p := u.Port(); p != "" {
				fmt.Sscanf(p, "%d", &v.Port)
			}
		}
	}
	if v.Port == 0 && it.Port > 0 {
		v.Port = it.Port
	}
	return v
}

// evaluateHoneypot 对已落库的服务资产做签名评分；命中时回写并写 fact。
func (t *ToolSet) evaluateHoneypot(assetID int64, it assetInputItem) {
	if t == nil || t.as == nil || assetID <= 0 {
		return
	}
	score, evidence := honeydetect.Evaluate(serviceViewFromInsert(it))
	if score <= 0 {
		return
	}
	if err := t.as.SetHoneypotScore(assetID, score, evidence); err != nil {
		return
	}
	if t.ts == nil {
		return
	}
	summary := fmt.Sprintf("资产 #%d 命中蜜罐静态签名（置信度 %.2f）", assetID, score)
	payload := map[string]any{
		"summary":    summary,
		"detail":     strings.Join(evidence, "; "),
		"evidence":   strings.Join(evidence, "; "),
		"confidence": "inferred",
	}
	id, err := t.ts.AddNode(db.KindFact, payload, 5, "confirmed", t.worker, []int64{assetID})
	if err != nil {
		return
	}
	if t.ownerNode > 0 {
		_ = t.ts.Link(t.ownerNode, db.RelYields, id)
	}
	t.writes.Facts++
}
