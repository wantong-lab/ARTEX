package db

// 数据保留与清理（M5-02）：activity 30d / server_logs 14d / C2 30d，均可经 settings 覆盖。
// 清理永远先算 counts；dry-run 只回 counts 不删任何行，真实清理在单事务里删并写审计。

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// RetentionPolicy 是三类数据的保留天数。
type RetentionPolicy struct {
	ActivityDays int `json:"activity_days"`
	LogsDays     int `json:"logs_days"`
	C2Days       int `json:"c2_days"`
}

// DefaultRetentionPolicy 是 ROADMAP M5-02 的默认策略。
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{ActivityDays: 30, LogsDays: 14, C2Days: 30}
}

func clampRetentionDays(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > 3650 {
		return 3650
	}
	return v
}

// Normalize 把非法值回落为默认并夹到 1..3650。
func (p RetentionPolicy) Normalize() RetentionPolicy {
	d := DefaultRetentionPolicy()
	return RetentionPolicy{
		ActivityDays: clampRetentionDays(p.ActivityDays, d.ActivityDays),
		LogsDays:     clampRetentionDays(p.LogsDays, d.LogsDays),
		C2Days:       clampRetentionDays(p.C2Days, d.C2Days),
	}
}

// LoadRetentionPolicy 从 settings 读取（缺失/非法回落默认）。
func (d *DB) LoadRetentionPolicy() RetentionPolicy {
	p := DefaultRetentionPolicy()
	read := func(key string) int {
		v, ok, err := d.GetSetting(key)
		if err != nil || !ok {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return n
	}
	p.ActivityDays = read("retention_activity_days")
	p.LogsDays = read("retention_logs_days")
	p.C2Days = read("retention_c2_days")
	return p.Normalize()
}

// SaveRetentionPolicy 持久化策略。
func (d *DB) SaveRetentionPolicy(p RetentionPolicy) error {
	p = p.Normalize()
	for k, v := range map[string]int{
		"retention_activity_days": p.ActivityDays,
		"retention_logs_days":     p.LogsDays,
		"retention_c2_days":       p.C2Days,
	} {
		if err := d.SetSetting(k, strconv.Itoa(v)); err != nil {
			return err
		}
	}
	return nil
}

// RetentionCounts 是各表「已到期」行数。
type RetentionCounts struct {
	Activity   int64 `json:"activity"`
	ServerLogs int64 `json:"server_logs"`
	C2Tasks    int64 `json:"c2_tasks"`
	C2Events   int64 `json:"c2_events"`
	C2Sessions int64 `json:"c2_sessions"`
}

func (c RetentionCounts) Empty() bool {
	return c.Activity == 0 && c.ServerLogs == 0 && c.C2Tasks == 0 && c.C2Events == 0 && c.C2Sessions == 0
}

// RetentionResult 是一次清理（或 dry-run）结果。
type RetentionResult struct {
	DryRun  bool            `json:"dry_run"`
	Policy  RetentionPolicy `json:"policy"`
	Counts  RetentionCounts `json:"counts"` // 到期条数（dry-run 结果）
	Deleted RetentionCounts `json:"deleted,omitempty"`
	AuditID int64           `json:"audit_id,omitempty"`
	At      time.Time       `json:"at"`
}

// RetentionAudit 是审计行。
type RetentionAudit struct {
	ID      int64           `json:"id"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	DryRun  bool            `json:"dry_run"`
	Policy  RetentionPolicy `json:"policy"`
	Deleted RetentionCounts `json:"deleted"`
}

func retentionCutoffs(p RetentionPolicy, now time.Time) (actCut, logCut, c2Cut time.Time) {
	return now.Add(-time.Duration(p.ActivityDays) * 24 * time.Hour),
		now.Add(-time.Duration(p.LogsDays) * 24 * time.Hour),
		now.Add(-time.Duration(p.C2Days) * 24 * time.Hour)
}

type retentionQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func countExpired(ctx context.Context, q retentionQueryer, p RetentionPolicy, now time.Time) (RetentionCounts, error) {
	actCut, logCut, c2Cut := retentionCutoffs(p, now)
	var c RetentionCounts
	rows := []struct {
		dst   *int64
		query string
		arg   time.Time
	}{
		{&c.Activity, "SELECT count(*) FROM activity WHERE created_at < $1", actCut},
		{&c.ServerLogs, "SELECT count(*) FROM server_logs WHERE created_at < $1", logCut},
		{&c.C2Tasks, "SELECT count(*) FROM c2_tasks WHERE created_at < $1 AND status IN ('success','failed','cancelled')", c2Cut},
		{&c.C2Events, "SELECT count(*) FROM c2_events WHERE created_at < $1", c2Cut},
		{&c.C2Sessions, "SELECT count(*) FROM c2_sessions WHERE status='dead' AND last_check_in < $1", c2Cut},
	}
	for _, r := range rows {
		if err := q.QueryRowContext(ctx, r.query, r.arg).Scan(r.dst); err != nil {
			return c, err
		}
	}
	return c, nil
}

// RetentionPreview 只算到期条数（dry-run 的数据来源）。
func (d *DB) RetentionPreview(ctx context.Context, p RetentionPolicy) (RetentionCounts, error) {
	p = p.Normalize()
	return countExpired(ctx, d, p, time.Now())
}

// RunRetentionCleanup 执行清理。dryRun=true 时只统计不删除、也不写审计。
func (d *DB) RunRetentionCleanup(ctx context.Context, p RetentionPolicy, actor string, dryRun bool) (*RetentionResult, error) {
	p = p.Normalize()
	now := time.Now()
	res := &RetentionResult{DryRun: dryRun, Policy: p, At: now}
	counts, err := d.RetentionPreview(ctx, p)
	if err != nil {
		return nil, err
	}
	res.Counts = counts
	if dryRun {
		return res, nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	actCut, logCut, c2Cut := retentionCutoffs(p, now)
	del := func(query string, arg time.Time) (int64, error) {
		r, err := tx.ExecContext(ctx, query, arg)
		if err != nil {
			return 0, err
		}
		return r.RowsAffected()
	}
	if res.Deleted.C2Tasks, err = del("DELETE FROM c2_tasks WHERE created_at < $1 AND status IN ('success','failed','cancelled')", c2Cut); err != nil {
		return nil, fmt.Errorf("删除 c2_tasks 失败: %w", err)
	}
	if res.Deleted.C2Events, err = del("DELETE FROM c2_events WHERE created_at < $1", c2Cut); err != nil {
		return nil, fmt.Errorf("删除 c2_events 失败: %w", err)
	}
	if res.Deleted.C2Sessions, err = del("DELETE FROM c2_sessions WHERE status='dead' AND last_check_in < $1", c2Cut); err != nil {
		return nil, fmt.Errorf("删除 c2_sessions 失败: %w", err)
	}
	if res.Deleted.Activity, err = del("DELETE FROM activity WHERE created_at < $1", actCut); err != nil {
		return nil, fmt.Errorf("删除 activity 失败: %w", err)
	}
	if res.Deleted.ServerLogs, err = del("DELETE FROM server_logs WHERE created_at < $1", logCut); err != nil {
		return nil, fmt.Errorf("删除 server_logs 失败: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO retention_audit(actor,dry_run,activity_days,logs_days,c2_days,
		activity_deleted,logs_deleted,c2_tasks_deleted,c2_events_deleted,c2_sessions_deleted)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		actor, dryRun, p.ActivityDays, p.LogsDays, p.C2Days,
		res.Deleted.Activity, res.Deleted.ServerLogs, res.Deleted.C2Tasks, res.Deleted.C2Events, res.Deleted.C2Sessions,
	).Scan(&res.AuditID); err != nil {
		return nil, fmt.Errorf("写清理审计失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// LastRetentionAudit 返回最近一次真实清理审计（无记录返回 nil）。
func (d *DB) LastRetentionAudit(ctx context.Context) (*RetentionAudit, error) {
	var a RetentionAudit
	var policyDays RetentionPolicy
	err := d.QueryRowContext(ctx, `SELECT id, at, actor, dry_run, activity_days, logs_days, c2_days,
		activity_deleted, logs_deleted, c2_tasks_deleted, c2_events_deleted, c2_sessions_deleted
		FROM retention_audit ORDER BY id DESC LIMIT 1`).Scan(
		&a.ID, &a.At, &a.Actor, &a.DryRun, &policyDays.ActivityDays, &policyDays.LogsDays, &policyDays.C2Days,
		&a.Deleted.Activity, &a.Deleted.ServerLogs, &a.Deleted.C2Tasks, &a.Deleted.C2Events, &a.Deleted.C2Sessions,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Policy = policyDays
	return &a, nil
}
