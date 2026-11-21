package db

// host_ledger 主机操作台账(recon 新增,内核级):
// 每次在目标/跳板/会话上的操作自动记账——执行了什么命令、建了什么文件/账号/后门、
// 拿到了什么凭据/权限,全都落库可查。不靠 AI 回忆,工具调用层确定性记录。

import "time"

type HostOp struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	Host      string    `json:"host"`
	Channel   string    `json:"channel"` // webshell|c2|ssh|vps|session
	Cmd       string    `json:"cmd"`
	Stdout    string    `json:"stdout,omitempty"`
	OK        bool      `json:"ok"`
	Ms        int       `json:"ms"`
	Artifact  bool      `json:"artifact"`                // 是否持久化痕迹(落盘/账号/key/服务)
	AtType    string    `json:"artifact_type,omitempty"` // file|account|sshkey|service|cron|cred|other
	Cleanup   string    `json:"cleanup_cmd,omitempty"`   // 回滚该痕迹的命令
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) ensureHostLedger() {
	_, _ = d.Exec(`CREATE TABLE IF NOT EXISTS host_ledger(
		id BIGSERIAL PRIMARY KEY, session_id BIGINT NOT NULL DEFAULT 0,
		host TEXT NOT NULL DEFAULT '', channel TEXT NOT NULL DEFAULT '',
		cmd TEXT NOT NULL DEFAULT '', stdout TEXT NOT NULL DEFAULT '',
		ok BOOLEAN NOT NULL DEFAULT true, ms INTEGER NOT NULL DEFAULT 0,
		artifact BOOLEAN NOT NULL DEFAULT false, artifact_type TEXT NOT NULL DEFAULT '',
		cleanup_cmd TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_host_ledger_session ON host_ledger(session_id)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_host_ledger_host ON host_ledger(host)`)
}

// AddHostOp 记一条操作(自动由 exec 路径调;artifact 由 recordHostArtifact 或命令启发式置位)。
func (d *DB) AddHostOp(o *HostOp) error {
	d.ensureHostLedger()
	_, err := d.Exec(`INSERT INTO host_ledger(session_id,host,channel,cmd,stdout,ok,ms,artifact,artifact_type,cleanup_cmd,note)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		o.SessionID, o.Host, o.Channel, o.Cmd, o.Stdout, o.OK, o.Ms,
		o.Artifact, o.AtType, o.Cleanup, o.Note)
	return err
}

func (d *DB) ListHostOps(sessionID int64, host string, artifactsOnly bool, limit int) ([]*HostOp, error) {
	d.ensureHostLedger()
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id,session_id,host,channel,cmd,stdout,ok,ms,artifact,artifact_type,cleanup_cmd,note,created_at
		FROM host_ledger WHERE 1=1`
	var args []any
	n := 0
	if sessionID > 0 {
		n++
		q += ` AND session_id=$` + itoa64(int64(n))
		args = append(args, sessionID)
	}
	if host != "" {
		n++
		q += ` AND host=$` + itoa64(int64(n))
		args = append(args, host)
	}
	if artifactsOnly {
		q += ` AND artifact`
	}
	q += ` ORDER BY id DESC LIMIT ` + itoa64(int64(limit))
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*HostOp
	for rows.Next() {
		o := &HostOp{}
		if err := rows.Scan(&o.ID, &o.SessionID, &o.Host, &o.Channel, &o.Cmd, &o.Stdout, &o.OK, &o.Ms, &o.Artifact, &o.AtType, &o.Cleanup, &o.Note, &o.CreatedAt); err == nil {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}
