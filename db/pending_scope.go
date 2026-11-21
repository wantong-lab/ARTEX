package db

// 越界范围申请（M2-04）：agent 发现新网段时提交申请，人工批准后写 task_scope。
// 新 IP 按 /24（IPv6 /64）聚合，避免逐 IP 审批；批准即时生效（guard 每次调用重读）。

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type PendingScope struct {
	ID          int64  `json:"id"`
	TaskID      int64  `json:"task_id"`
	Kind        string `json:"kind"`
	Value       string `json:"value"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
	ReviewedBy  string `json:"reviewed_by"`
}

type PendingScopeStore struct {
	d *DB
}

func NewPendingScopeStore(d *DB) *PendingScopeStore { return &PendingScopeStore{d: d} }

// aggregateScopeValue 把申请值归一化：ip → 所在网段（IPv4 /24、IPv6 /64），
// domain 小写去尾点；cidr 原样校验。
func aggregateScopeValue(kind, value string) (string, string, error) {
	v := strings.TrimSpace(value)
	switch strings.ToLower(kind) {
	case "ip":
		ip := net.ParseIP(v)
		if ip == nil {
			return "", "", fmt.Errorf("非法 IP: %q", value)
		}
		if ip4 := ip.To4(); ip4 != nil {
			mask := net.CIDRMask(24, 32)
			return "cidr", (&net.IPNet{IP: ip4.Mask(mask), Mask: mask}).String(), nil
		}
		mask := net.CIDRMask(64, 128)
		return "cidr", (&net.IPNet{IP: ip.Mask(mask), Mask: mask}).String(), nil
	case "cidr":
		if _, _, err := net.ParseCIDR(v); err != nil {
			return "", "", fmt.Errorf("非法 CIDR: %q", value)
		}
		return "cidr", v, nil
	case "domain":
		v = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v), "."))
		if v == "" {
			return "", "", fmt.Errorf("domain 为空")
		}
		return "domain", v, nil
	}
	return "", "", fmt.Errorf("kind 必须是 ip/cidr/domain")
}

// Create 提交一条申请（幂等：同 task/kind/value 重复提交更新原因并返回已有行 id）。
func (s *PendingScopeStore) Create(ctx context.Context, taskID int64, kind, value, reason, requestedBy string) (int64, string, error) {
	if taskID <= 0 {
		return 0, "", fmt.Errorf("task_id 无效")
	}
	kind, value, err := aggregateScopeValue(kind, value)
	if err != nil {
		return 0, "", err
	}
	if requestedBy == "" {
		requestedBy = "agent"
	}
	var id int64
	err = s.d.QueryRowContext(ctx, `INSERT INTO pending_scope(task_id, kind, value, reason, requested_by)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (task_id, kind, value) DO UPDATE SET reason = EXCLUDED.reason
		RETURNING id`, taskID, kind, value, reason, requestedBy).Scan(&id)
	if err != nil {
		return 0, value, err
	}
	return id, value, nil
}

// List 按 status（空=全部）与 taskID（0=全部）列申请。
func (s *PendingScopeStore) List(ctx context.Context, taskID int64, status string) ([]*PendingScope, error) {
	q := `SELECT id, task_id, kind, value, reason, status, requested_by, reviewed_by FROM pending_scope WHERE 1=1`
	var args []any
	if taskID > 0 {
		args = append(args, taskID)
		q += fmt.Sprintf(" AND task_id=$%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(" AND status=$%d", len(args))
	}
	q += " ORDER BY id"
	rows, err := s.d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PendingScope{}
	for rows.Next() {
		var p PendingScope
		if err := rows.Scan(&p.ID, &p.TaskID, &p.Kind, &p.Value, &p.Reason, &p.Status, &p.RequestedBy, &p.ReviewedBy); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// Approve 批准申请：先写 task_scope（source=approval）再置 approved——保证「approved
// 一定已生效」；scope 写失败则保持 pending。
func (s *PendingScopeStore) Approve(ctx context.Context, id int64, reviewer string, assets *AssetStore) error {
	var taskID int64
	var kind, value, status string
	err := s.d.QueryRowContext(ctx, `SELECT task_id, kind, value, status FROM pending_scope WHERE id=$1`, id).Scan(&taskID, &kind, &value, &status)
	if err != nil {
		return err
	}
	if status != "pending" {
		return fmt.Errorf("申请 %d 状态为 %s，不可重复审批", id, status)
	}
	if assets == nil {
		return fmt.Errorf("AssetStore 未初始化")
	}
	if _, err := assets.AddAgentScope(taskID, kind, value, "范围审批通过："+reviewer, "approval"); err != nil {
		return fmt.Errorf("写入任务范围失败: %w", err)
	}
	_, err = s.d.ExecContext(ctx, "UPDATE pending_scope SET status='approved', reviewed_by=$2, reviewed_at=now() WHERE id=$1 AND status='pending'", id, reviewer)
	return err
}

// Reject 拒绝申请。
func (s *PendingScopeStore) Reject(ctx context.Context, id int64, reviewer string) error {
	res, err := s.d.ExecContext(ctx, "UPDATE pending_scope SET status='rejected', reviewed_by=$2, reviewed_at=now() WHERE id=$1 AND status='pending'", id, reviewer)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("申请 %d 不存在或已审批", id)
	}
	return nil
}
