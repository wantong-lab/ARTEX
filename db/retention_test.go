package db

import (
	"testing"
)

// TestRetentionDryRunAndCleanup 验证：dry-run 只计数不删不写审计；真实清理只删到期数据；
// 清理后写审计且未到期数据仍在。
func TestRetentionDryRunAndCleanup(t *testing.T) {
	d := openC2TestDB(t)
	ctx := t.Context()
	// 旧/新后端日志
	if _, err := d.Exec(`INSERT INTO server_logs(created_at,level,text) VALUES
		(now()-interval '20 days','info','old log'),(now(),'info','new log')`); err != nil {
		t.Fatalf("insert logs: %v", err)
	}
	// 旧/新 activity（依赖真实 exploration）
	task, err := d.CreateTask("retention test", "goal", nil, 0, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO activity(exploration_id,created_at,summary) VALUES
		($1, now()-interval '40 days','old act'),($1, now(),'new act')`, task.ExplorationID); err != nil {
		t.Fatalf("insert activity: %v", err)
	}
	// 旧/新 C2 台账
	var lid int64
	if err := d.QueryRow(`INSERT INTO c2_listeners(name,bind_host,bind_port) VALUES('l','127.0.0.1',1) RETURNING id`).Scan(&lid); err != nil {
		t.Fatalf("insert listener: %v", err)
	}
	var oldSess, newSess int64
	if err := d.QueryRow(`INSERT INTO c2_sessions(listener_id,implant_uuid,status,last_check_in)
		VALUES($1,'old','dead', now()-interval '40 days') RETURNING id`, lid).Scan(&oldSess); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if err := d.QueryRow(`INSERT INTO c2_sessions(listener_id,implant_uuid,status)
		VALUES($1,'new','active') RETURNING id`, lid).Scan(&newSess); err != nil {
		t.Fatalf("insert new session: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO c2_tasks(session_id,status,created_at) VALUES
		($1,'success', now()-interval '40 days'),($1,'success', now())`, newSess); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO c2_events(created_at,message) VALUES
		(now()-interval '40 days','old evt'),(now(),'new evt')`); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	p := RetentionPolicy{ActivityDays: 30, LogsDays: 14, C2Days: 30}
	preview, err := d.RetentionPreview(ctx, p)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.ServerLogs != 1 || preview.Activity != 1 || preview.C2Tasks != 1 || preview.C2Events != 1 || preview.C2Sessions != 1 {
		t.Fatalf("preview=%+v", preview)
	}

	// dry-run：只计数，不删、不写审计
	dry, err := d.RunRetentionCleanup(ctx, p, "tester", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || (dry.Deleted != RetentionCounts{}) || dry.AuditID != 0 {
		t.Fatalf("dry run result=%+v", dry)
	}
	if again, _ := d.RetentionPreview(ctx, p); again != preview {
		t.Fatalf("dry run must not delete: before=%+v after=%+v", preview, again)
	}
	if last, _ := d.LastRetentionAudit(ctx); last != nil {
		t.Fatalf("dry run must not write audit: %+v", last)
	}

	// 真实清理：只删到期数据
	real, err := d.RunRetentionCleanup(ctx, p, "tester", false)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if real.Deleted != preview {
		t.Fatalf("deleted=%+v preview=%+v", real.Deleted, preview)
	}
	if real.AuditID <= 0 {
		t.Fatal("real cleanup must write audit row")
	}
	if after, _ := d.RetentionPreview(ctx, p); !after.Empty() {
		t.Fatalf("post-cleanup preview should be empty: %+v", after)
	}
	var logs, acts int
	_ = d.QueryRow(`SELECT count(*) FROM server_logs`).Scan(&logs)
	_ = d.QueryRow(`SELECT count(*) FROM activity`).Scan(&acts)
	if logs != 1 || acts != 1 {
		t.Fatalf("fresh rows must survive: logs=%d activity=%d", logs, acts)
	}
	last, err := d.LastRetentionAudit(ctx)
	if err != nil || last == nil {
		t.Fatalf("last audit: %v %v", last, err)
	}
	if last.Actor != "tester" || last.DryRun || last.Policy.ActivityDays != 30 {
		t.Fatalf("audit=%+v", last)
	}
}

// TestRetentionPolicyNormalize 验证非法策略回落默认并夹取上限。
func TestRetentionPolicyNormalize(t *testing.T) {
	p := RetentionPolicy{ActivityDays: -1, LogsDays: 99999, C2Days: 0}.Normalize()
	if p.ActivityDays != 30 || p.LogsDays != 3650 || p.C2Days != 30 {
		t.Fatalf("normalize=%+v", p)
	}
}
