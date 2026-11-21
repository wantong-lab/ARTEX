package agent

import (
	"strings"
	"testing"
)

func TestAppendPlannerExtras(t *testing.T) {
	base := "SYSTEM"
	if got := appendPlannerExtras(base, nil); got != base {
		t.Fatalf("nil extra changed prompt: %q", got)
	}
	if got := appendPlannerExtras(base, func() string { return "  " }); got != base {
		t.Fatalf("blank extra changed prompt: %q", got)
	}
	got := appendPlannerExtras(base, func() string { return "知识库：共 3 条" })
	if !strings.Contains(got, "平台知识弹药") || !strings.Contains(got, "知识库：共 3 条") {
		t.Fatalf("extra not injected: %q", got)
	}
}
