package kb

import (
	"testing"
)

func TestEntriesPageUpdateDeleteInsights(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()
	seed := []*Entry{
		{ID: "kb-a", Title: "A 打法", Category: "tactic", VulnClass: "rce", Content: "step a"},
		{ID: "kb-b", Title: "B 弹药", Category: "ammo", VulnClass: "sqli", Content: "step b"},
		{ID: "kb-c", Title: "C 负面", Category: "negative", VulnClass: "upload", Content: "dead end"},
	}
	for _, e := range seed {
		if _, err := c.Add(e); err != nil {
			t.Fatalf("add %s: %v", e.ID, err)
		}
	}
	page, err := c.ListPage(0, 2, "")
	if err != nil || page.Total != 3 || len(page.Entries) != 2 {
		t.Fatalf("page1=%+v err=%v", page, err)
	}
	page2, err := c.ListPage(2, 2, "")
	if err != nil || len(page2.Entries) != 1 {
		t.Fatalf("page2 len=%d err=%v", len(page2.Entries), err)
	}
	tac, err := c.ListPage(0, 10, "tactic")
	if err != nil || tac.Total != 1 || len(tac.Entries) != 1 || tac.Entries[0].ID != "kb-a" {
		t.Fatalf("tactic page=%+v err=%v", tac, err)
	}
	if err := c.Update(&Entry{ID: "kb-a", Title: "A 更新", Category: "tactic", VulnClass: "rce", Content: "step a2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := c.Get("kb-a")
	if err != nil || got == nil || got.Title != "A 更新" || got.Content != "step a2" {
		t.Fatalf("get after update=%+v err=%v", got, err)
	}
	if err := c.Update(&Entry{ID: "missing", Title: "x", Content: "y"}); err == nil {
		t.Fatal("update missing should error")
	}
	ins, err := c.Insights()
	if err != nil || ins.Total != 3 || ins.ByCategory["tactic"] != 1 || ins.ByCategory["ammo"] != 1 || ins.ByCategory["negative"] != 1 {
		t.Fatalf("insights=%+v err=%v", ins, err)
	}
	if len(ins.Recent) != 3 {
		t.Fatalf("recent=%d", len(ins.Recent))
	}
	if err := c.Delete("kb-b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if e, _ := c.Get("kb-b"); e != nil {
		t.Fatal("deleted entry still readable")
	}
	if err := c.Delete("kb-b"); err == nil {
		t.Fatal("delete missing should error")
	}
	after, err := c.ListPage(0, 10, "")
	if err != nil || after.Total != 2 {
		t.Fatalf("after delete total=%d err=%v", after.Total, err)
	}
}
