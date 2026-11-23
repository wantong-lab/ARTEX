package db

// c2_ingress.go 是 C2 入站中继的节点台账（P2-C）：按 tunnel_id 唯一，
// 记录 VPS 回调地址、节点标签、健康状态与错误，供列表/巡检/手动重建使用。

import (
	"context"
	"database/sql"
	"time"
)

type C2IngressRecord struct {
	ID           int64     "json:\"id\""
	TunnelID     int64     "json:\"tunnel_id\""
	ListenerID   int64     "json:\"listener_id\""
	TaskID       int64     "json:\"task_id\""
	VPSHost      string    "json:\"vps_host\""
	VPSPort      int       "json:\"vps_port\""
	NodeLabel    string    "json:\"node_label\""
	Status       string    "json:\"status\""
	Healthy      bool      "json:\"healthy\""
	LastHealthAt time.Time "json:\"last_health_at\""
	LastError    string    "json:\"last_error\""
	CreatedAt    time.Time "json:\"created_at\""
	StoppedAt    time.Time "json:\"stopped_at\""
}

type C2IngressStore struct{ d *DB }

func NewC2IngressStore(d *DB) *C2IngressStore { return &C2IngressStore{d: d} }

func (s *C2IngressStore) Upsert(ctx context.Context, r *C2IngressRecord) (int64, error) {
	status := r.Status
	if status == "" {
		status = "deploying"
	}
	var id int64
	err := s.d.QueryRowContext(ctx, `
INSERT INTO c2_ingress(tunnel_id,listener_id,task_id,vps_host,vps_port,node_label,status,healthy,last_error,stopped_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'',NULL)
ON CONFLICT(tunnel_id) DO UPDATE SET
 listener_id=EXCLUDED.listener_id, task_id=EXCLUDED.task_id,
 vps_host=EXCLUDED.vps_host, vps_port=EXCLUDED.vps_port,
 node_label=EXCLUDED.node_label, status=EXCLUDED.status, healthy=false,
 last_error='', stopped_at=NULL
RETURNING id`, r.TunnelID, r.ListenerID, r.TaskID, r.VPSHost, r.VPSPort, r.NodeLabel, status, r.Healthy).Scan(&id)
	return id, err
}

const c2IngressCols = `id,tunnel_id,listener_id,task_id,vps_host,vps_port,node_label,status,healthy,last_health_at,last_error,created_at,stopped_at`

func scanC2Ingress(row interface{ Scan(...any) error }) (*C2IngressRecord, error) {
	var r C2IngressRecord
	var lastHealth, stopped sql.NullTime
	if err := row.Scan(&r.ID, &r.TunnelID, &r.ListenerID, &r.TaskID, &r.VPSHost, &r.VPSPort,
		&r.NodeLabel, &r.Status, &r.Healthy, &lastHealth, &r.LastError, &r.CreatedAt, &stopped); err != nil {
		return nil, err
	}
	if lastHealth.Valid {
		r.LastHealthAt = lastHealth.Time
	}
	if stopped.Valid {
		r.StoppedAt = stopped.Time
	}
	return &r, nil
}

func (s *C2IngressStore) GetByTunnel(ctx context.Context, tunnelID int64) (*C2IngressRecord, error) {
	r, err := scanC2Ingress(s.d.QueryRowContext(ctx, `SELECT `+c2IngressCols+` FROM c2_ingress WHERE tunnel_id=$1`, tunnelID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *C2IngressStore) ListActive(ctx context.Context) ([]*C2IngressRecord, error) {
	rows, err := s.d.QueryContext(ctx, `SELECT `+c2IngressCols+` FROM c2_ingress WHERE stopped_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2IngressRecord
	for rows.Next() {
		r, serr := scanC2Ingress(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *C2IngressStore) UpdateHealth(ctx context.Context, tunnelID int64, healthy bool, status, errMsg string) error {
	_, err := s.d.ExecContext(ctx, `UPDATE c2_ingress SET healthy=$2,status=$3,last_error=$4,last_health_at=now()
		WHERE tunnel_id=$1 AND stopped_at IS NULL`, tunnelID, healthy, status, errMsg)
	return err
}

func (s *C2IngressStore) MarkStopped(ctx context.Context, tunnelID int64) error {
	_, err := s.d.ExecContext(ctx, `UPDATE c2_ingress SET stopped_at=now(),healthy=false,status='stopped',last_health_at=now()
		WHERE tunnel_id=$1`, tunnelID)
	return err
}
