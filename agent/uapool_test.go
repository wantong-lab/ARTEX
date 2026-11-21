package agent

import "testing"

func TestUAPoolFromSettingAndPick(t *testing.T) {
	pool := UAPoolFromSetting(`["A","B"]`)
	if len(pool) != 2 {
		t.Fatalf("pool=%v", pool)
	}
	if PickUA(pool, 7) != PickUA(pool, 7) {
		t.Fatal("UA must be stable per task")
	}
}
