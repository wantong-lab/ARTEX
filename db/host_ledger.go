package db

// host_ledger 主机操作台账(recon 新增,内核级):
// 每次在目标/跳板/会话上的操作自动记账——执行了什么命令、建了什么文件/账号/后门、
// 拿到了什么凭据/权限,全都落库可查。不靠 AI 回忆,工具调用层确定性记录。

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

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
	_, _ = d.Exec(`ALTER TABLE host_ledger ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE host_ledger ADD COLUMN IF NOT EXISTS entry_hash TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_host_ledger_session ON host_ledger(session_id)`)
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_host_ledger_host ON host_ledger(host)`)
}

// hostOpDigest 计算一条台账的链式哈希：prev_hash + 全部可审计字段，字段间用 NUL
// 分隔避免拼接歧义。created_at 不参与（DB 默认值，读回可用于展示但不作为链输入）。
func hostOpDigest(prev string, o *HostOp) string {
	h := sha256.New()
	fields := []string{
		prev, strconv.FormatInt(o.SessionID, 10), o.Host, o.Channel, o.Cmd, o.Stdout,
		strconv.FormatBool(o.OK), strconv.Itoa(o.Ms), strconv.FormatBool(o.Artifact),
		o.AtType, o.Cleanup, o.Note,
	}
	for _, f := range fields {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AddHostOp 记一条操作(自动由 exec 路径调;artifact 由 recordHostArtifact 或命令启发式置位)。
// 写入时在同一事务里锁表、读取上一行 entry_hash 并写入本行哈希，形成防篡改链。
func (d *DB) AddHostOp(o *HostOp) error {
	d.ensureHostLedger()
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`LOCK TABLE host_ledger IN EXCLUSIVE MODE`); err != nil {
		return err
	}
	var prev string
	err = tx.QueryRow(`SELECT COALESCE(entry_hash,'') FROM host_ledger ORDER BY id DESC LIMIT 1`).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	digest := hostOpDigest(prev, o)
	if _, err := tx.Exec(`INSERT INTO host_ledger(session_id,host,channel,cmd,stdout,ok,ms,artifact,artifact_type,cleanup_cmd,note,prev_hash,entry_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		o.SessionID, o.Host, o.Channel, o.Cmd, o.Stdout, o.OK, o.Ms,
		o.Artifact, o.AtType, o.Cleanup, o.Note, prev, digest); err != nil {
		return err
	}
	return tx.Commit()
}

// AuditVerifyResult 是 host_ledger 哈希链校验结果。
type AuditVerifyResult struct {
	OK       bool   `json:"ok"`
	Total    int    `json:"total"`
	Hashed   int    `json:"hashed"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// VerifyHostLedger 校验 host_ledger 的哈希链。升级前的历史行（entry_hash 为空）
// 不参与链，从第一条有哈希的行开始校验；任何中间行被改/删/插入都会返回
// broken_at 与原因。
func (d *DB) VerifyHostLedger() (*AuditVerifyResult, error) {
	d.ensureHostLedger()
	rows, err := d.Query(`SELECT id,session_id,host,channel,cmd,stdout,ok,ms,artifact,artifact_type,cleanup_cmd,note,COALESCE(prev_hash,''),COALESCE(entry_hash,'')
		FROM host_ledger ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &AuditVerifyResult{}
	prev := ""
	started := false
	for rows.Next() {
		o := &HostOp{}
		var prevHash, entryHash string
		if err := rows.Scan(&o.ID, &o.SessionID, &o.Host, &o.Channel, &o.Cmd, &o.Stdout, &o.OK, &o.Ms,
			&o.Artifact, &o.AtType, &o.Cleanup, &o.Note, &prevHash, &entryHash); err != nil {
			return nil, err
		}
		res.Total++
		if entryHash == "" {
			if started {
				res.BrokenAt = o.ID
				res.Reason = "链中出现了无哈希的历史行（可能被插入/删除）"
				return res, nil
			}
			continue
		}
		if !started {
			started = true
			prev = prevHash
		}
		if prevHash != prev {
			res.BrokenAt = o.ID
			res.Reason = "prev_hash 与上一行 entry_hash 不一致"
			return res, nil
		}
		want := hostOpDigest(prev, o)
		if want != entryHash {
			res.BrokenAt = o.ID
			res.Reason = "entry_hash 校验失败（行内容被修改）"
			return res, nil
		}
		prev = entryHash
		res.Hashed++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.OK = true
	return res, nil
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
