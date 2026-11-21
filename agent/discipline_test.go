package agent

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func withPromptOverride(t *testing.T, body string) {
	t.Helper()
	t.Cleanup(func() { PromptOverride = nil })
	PromptOverride = func(string) (string, bool) { return body, true }
}

// 固定尾必须由代码拥有：DB 正文改成任意内容后仍然存在。
func TestWorkerFixedTailsSurviveOverride(t *testing.T) {
	withPromptOverride(t, "WORKER BODY")
	got := workerSystem("", "", "/data", "/data/run")
	if !strings.HasPrefix(got, "WORKER BODY") {
		t.Fatalf("body not honored: %q", got)
	}
	for _, want := range []string{"meta 铁律", "证据先行规则", "里程碑收尾规则", "反 AI 蜜罐红线"} {
		if !strings.Contains(got, want) {
			t.Fatalf("worker fixed tail missing %q", want)
		}
	}
}

func TestPlannerFixedTailsSurviveOverride(t *testing.T) {
	withPromptOverride(t, "PLANNER BODY")
	got := plannerSystem("goal", "/data", "/data/work")
	if !strings.HasPrefix(got, "PLANNER BODY") {
		t.Fatalf("body not honored: %q", got)
	}
	for _, want := range []string{"meta 铁律", "收尾痕迹清理纪律"} {
		if !strings.Contains(got, want) {
			t.Fatalf("planner fixed tail missing %q", want)
		}
	}
}

func TestRenderIntentTaskInjectsChainSkeleton(t *testing.T) {
	intent := &db.Node{ID: 7, Payload: []byte(`{"summary":"验证 SQL 注入","chain_tags":["web"]}`)}
	got := renderIntentTask(intent)
	if !strings.Contains(got, "意图 id: 7") {
		t.Fatalf("intent header missing: %q", got)
	}
	if !strings.Contains(got, `<chains-skeleton`) || !strings.Contains(got, `category="web"`) {
		t.Fatalf("web skeleton not injected: %q", got)
	}
	if !strings.Contains(got, "L4 原文指引") {
		t.Fatalf("refs guide not injected: %q", got)
	}
}

func TestRenderIntentTaskSilentWithoutCategory(t *testing.T) {
	intent := &db.Node{ID: 8, Payload: []byte(`{"summary":"整理工作台目录"}`)}
	got := renderIntentTask(intent)
	if strings.Contains(got, "<chains-skeleton") {
		t.Fatalf("skeleton must not be injected without a matching category: %q", got)
	}
	if !strings.Contains(got, "意图 id: 8") {
		t.Fatalf("intent header missing: %q", got)
	}
}

func TestNormalizeIntentChainTags(t *testing.T) {
	got, err := normalizeIntentChainTags([]string{" Web ", "web", "PIVOT"})
	if err != nil {
		t.Fatalf("valid tags rejected: %v", err)
	}
	if len(got) != 2 || got[0] != "web" || got[1] != "pivot" {
		t.Fatalf("normalize result=%v", got)
	}
	if got, err := normalizeIntentChainTags(nil); err != nil || got != nil {
		t.Fatalf("empty tags should pass: got=%v err=%v", got, err)
	}
	if _, err := normalizeIntentChainTags([]string{"cloud"}); err == nil {
		t.Fatal("invalid tag must be rejected")
	}
	if _, err := normalizeIntentChainTags([]string{"web", "ad", "priv"}); err == nil {
		t.Fatal("more than two tags must be rejected")
	}
}
