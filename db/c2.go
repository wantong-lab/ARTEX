package db

// C2 台账（M4-01）：监听器 / 会话 / 任务 / 事件。
// 监听器 token 用 AES-256-GCM 加密存储（gcm1: 前缀，密钥由 jwt.key 域分离派生）。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const (
	C2ListenerStopped = "stopped"
	C2ListenerRunning = "running"
	C2ListenerError   = "error"

	C2SessionActive = "active"
	C2SessionDead   = "dead"

	C2ListenerKindTCP  = "tcp"
	C2ListenerKindHTTP = "http"

	C2TaskQueued  = "queued"
	C2TaskSuccess = "success"
	C2TaskFailed  = "failed"
)

type C2Listener struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	BindHost  string    `json:"bind_host"`
	BindPort  int       `json:"bind_port"`
	Kind      string    `json:"kind"`
	Token     string    `json:"-"`
	Status    string    `json:"status"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

type C2Session struct {
	ID          int64     `json:"id"`
	ListenerID  int64     `json:"listener_id"`
	ImplantUUID string    `json:"implant_uuid"`
	RemoteAddr  string    `json:"remote_addr"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	Username    string    `json:"username"`
	Status      string    `json:"status"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastCheckIn time.Time `json:"last_check_in"`
}

type C2Task struct {
	ID         int64     `json:"id"`
	SessionID  int64     `json:"session_id"`
	TaskType   string    `json:"task_type"`
	Args       string    `json:"args"`
	Status     string    `json:"status"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
	DurationMs int64     `json:"duration_ms"`
}

type C2Store struct {
	d   *DB
	key []byte
}

// NewC2Store 构造访问层；masterKey 为 jwt.key 内容（空则加解密失败，诚实报错）。
// C2ListenerSecretKey 由 master key 派生 C2 监听器 token 加密密钥（SHA-256 域分离）。
func C2ListenerSecretKey(master []byte) []byte {
	sum := sha256.Sum256(append([]byte("artex/c2/listener/v1\x00"), master...))
	return sum[:]
}

func NewC2Store(d *DB, masterKey []byte) *C2Store {
	var key []byte
	if len(masterKey) > 0 {
		key = C2ListenerSecretKey(masterKey)
	}
	return &C2Store{d: d, key: key}
}

func (s *C2Store) seal(plain string) (string, error) {
	if len(s.key) == 0 {
		return "", fmt.Errorf("c2 密钥未初始化")
	}
	g, err := newGCM(s.key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *C2Store) openSealed(blob string) (string, error) {
	if len(s.key) == 0 {
		return "", fmt.Errorf("c2 密钥未初始化")
	}
	if !strings.HasPrefix(blob, secretPrefix) {
		return "", fmt.Errorf("c2 token 密文格式非法")
	}
	raw, err := base64.StdEncoding.DecodeString(blob[len(secretPrefix):])
	if err != nil {
		return "", err
	}
	g, err := newGCM(s.key)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", fmt.Errorf("c2 token 密文过短")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// CreateListener 新建监听器（token 加密落库）。
func (s *C2Store) CreateListener(ctx context.Context, l *C2Listener) (int64, error) {
	if l.Kind == "" {
		l.Kind = C2ListenerKindTCP
	}
	enc, err := s.seal(l.Token)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.d.QueryRowContext(ctx, `INSERT INTO c2_listeners(name,kind,bind_host,bind_port,token_enc)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, l.Name, l.Kind, l.BindHost, l.BindPort, enc).Scan(&id)
	return id, err
}

const c2ListenerCols = `id,name,kind,bind_host,bind_port,token_enc,status,last_error,created_at,started_at`

func scanC2Listener(row interface{ Scan(...any) error }, s *C2Store) (*C2Listener, error) {
	var l C2Listener
	var enc string
	var started sql.NullTime
	if err := row.Scan(&l.ID, &l.Name, &l.Kind, &l.BindHost, &l.BindPort, &enc, &l.Status, &l.LastError, &l.CreatedAt, &started); err != nil {
		return nil, err
	}
	if started.Valid {
		l.StartedAt = started.Time
	}
	if tok, err := s.openSealed(enc); err == nil {
		l.Token = tok
	} // 解密失败：列表仍可见，token 留空（单行降级）
	return &l, nil
}

func (s *C2Store) ListListeners(ctx context.Context) ([]*C2Listener, error) {
	rows, err := s.d.QueryContext(ctx, `SELECT `+c2ListenerCols+` FROM c2_listeners ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Listener
	for rows.Next() {
		l, err := scanC2Listener(rows, s)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *C2Store) GetListener(ctx context.Context, id int64) (*C2Listener, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+c2ListenerCols+` FROM c2_listeners WHERE id=$1`, id)
	l, err := scanC2Listener(row, s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *C2Store) SetListenerStatus(ctx context.Context, id int64, status, lastErr string) error {
	if status == C2ListenerRunning {
		_, err := s.d.ExecContext(ctx, `UPDATE c2_listeners SET status=$2,last_error=$3,started_at=now() WHERE id=$1`, id, status, lastErr)
		return err
	}
	_, err := s.d.ExecContext(ctx, `UPDATE c2_listeners SET status=$2,last_error=$3 WHERE id=$1`, id, status, lastErr)
	return err
}

func (s *C2Store) DeleteListener(ctx context.Context, id int64) error {
	_, err := s.d.ExecContext(ctx, `DELETE FROM c2_listeners WHERE id=$1`, id)
	return err
}

// UpsertSession 会话上线/心跳（按 listener_id+implant_uuid 去重）。
func (s *C2Store) UpsertSession(ctx context.Context, sess *C2Session) (int64, error) {
	var id int64
	err := s.d.QueryRowContext(ctx, `INSERT INTO c2_sessions(listener_id,implant_uuid,remote_addr,os,arch,username,status)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(listener_id,implant_uuid) DO UPDATE SET remote_addr=EXCLUDED.remote_addr,
		os=EXCLUDED.os,arch=EXCLUDED.arch,username=EXCLUDED.username,status=EXCLUDED.status,last_check_in=now()
		RETURNING id`,
		sess.ListenerID, sess.ImplantUUID, sess.RemoteAddr, sess.OS, sess.Arch, sess.Username, sess.Status).Scan(&id)
	return id, err
}

func (s *C2Store) ListSessions(ctx context.Context, activeOnly bool) ([]*C2Session, error) {
	q := `SELECT id,listener_id,implant_uuid,remote_addr,os,arch,username,status,first_seen_at,last_check_in FROM c2_sessions`
	if activeOnly {
		q += ` WHERE status='active'`
	}
	q += ` ORDER BY id`
	rows, err := s.d.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Session
	for rows.Next() {
		var c C2Session
		if err := rows.Scan(&c.ID, &c.ListenerID, &c.ImplantUUID, &c.RemoteAddr, &c.OS, &c.Arch, &c.Username, &c.Status, &c.FirstSeenAt, &c.LastCheckIn); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (s *C2Store) SetSessionStatus(ctx context.Context, id int64, status string) error {
	_, err := s.d.ExecContext(ctx, `UPDATE c2_sessions SET status=$2 WHERE id=$1`, id, status)
	return err
}

// CreateTask 建一条任务（queued；终态由调用方 FinishTask 推进）。
func (s *C2Store) CreateTask(ctx context.Context, sessionID int64, taskType, args, source string) (int64, error) {
	var id int64
	err := s.d.QueryRowContext(ctx, `INSERT INTO c2_tasks(session_id,task_type,args,source,status)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, sessionID, taskType, args, source, C2TaskQueued).Scan(&id)
	return id, err
}

func (s *C2Store) GetTask(ctx context.Context, id int64) (*C2Task, error) {
	var t C2Task
	err := s.d.QueryRowContext(ctx, `SELECT id,session_id,task_type,args,status,output,error,source,created_at,duration_ms
		FROM c2_tasks WHERE id=$1`, id).Scan(&t.ID, &t.SessionID, &t.TaskType, &t.Args, &t.Status, &t.Output, &t.Error, &t.Source, &t.CreatedAt, &t.DurationMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &t, err
}

func (s *C2Store) ListTasks(ctx context.Context, sessionID int64, limit int) ([]*C2Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.d.QueryContext(ctx, `SELECT id,session_id,task_type,args,status,output,error,source,created_at,duration_ms
		FROM c2_tasks WHERE ($1=0 OR session_id=$1) ORDER BY id DESC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Task
	for rows.Next() {
		var t C2Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.TaskType, &t.Args, &t.Status, &t.Output, &t.Error, &t.Source, &t.CreatedAt, &t.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// FinishTask 回写任务终态。
func (s *C2Store) FinishTask(ctx context.Context, id int64, status, output, errMsg string, durationMs int64) error {
	_, err := s.d.ExecContext(ctx, `UPDATE c2_tasks SET status=$2,output=$3,error=$4,duration_ms=$5,completed_at=now() WHERE id=$1`,
		id, status, output, errMsg, durationMs)
	return err
}

// AddEvent 记一条 C2 审计事件（尽力而为）。
func (s *C2Store) AddEvent(ctx context.Context, category, message, level string, listenerID, sessionID, taskID int64) {
	_, _ = s.d.ExecContext(ctx, `INSERT INTO c2_events(category,message,level,listener_id,session_id,task_id)
		VALUES($1,$2,$3,NULLIF($4,0),NULLIF($5,0),NULLIF($6,0))`, category, message, level, listenerID, sessionID, taskID)
}

// C2Event 是 c2_events 的一行（列表中只暴露展示字段）。
type C2Event struct {
	ID         int64     "json:\"id\""
	Category   string    "json:\"category\""
	Message    string    "json:\"message\""
	Level      string    "json:\"level\""
	ListenerID int64     "json:\"listener_id,omitempty\""
	SessionID  int64     "json:\"session_id,omitempty\""
	TaskID     int64     "json:\"task_id,omitempty\""
	CreatedAt  time.Time "json:\"created_at\""
}

// ListEvents 返回最近事件；category 为空表示全部。
func (s *C2Store) ListEvents(ctx context.Context, category string, limit int) ([]*C2Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.d.QueryContext(ctx, `SELECT id,category,message,level,COALESCE(listener_id,0),COALESCE(session_id,0),COALESCE(task_id,0),created_at
		FROM c2_events WHERE ($1 = '' OR category = $1) ORDER BY id DESC LIMIT $2`, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Event
	for rows.Next() {
		var e C2Event
		if err := rows.Scan(&e.ID, &e.Category, &e.Message, &e.Level, &e.ListenerID, &e.SessionID, &e.TaskID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// GetSession 按 id 取会话（HTTP beacon 用）。
func (s *C2Store) GetSession(ctx context.Context, id int64) (*C2Session, error) {
	var c C2Session
	err := s.d.QueryRowContext(ctx, `SELECT id,listener_id,implant_uuid,remote_addr,os,arch,username,status,first_seen_at,last_check_in
		FROM c2_sessions WHERE id=$1`, id).Scan(&c.ID, &c.ListenerID, &c.ImplantUUID, &c.RemoteAddr, &c.OS, &c.Arch, &c.Username, &c.Status, &c.FirstSeenAt, &c.LastCheckIn)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// NextQueuedTask 原子领取该会话最早的一条 queued 任务（置 sent）；无任务返回 (nil,nil)。
func (s *C2Store) NextQueuedTask(ctx context.Context, sessionID int64) (*C2Task, error) {
	var t C2Task
	err := s.d.QueryRowContext(ctx, `UPDATE c2_tasks SET status='sent' WHERE id=(
			SELECT id FROM c2_tasks WHERE session_id=$1 AND status='queued' ORDER BY id LIMIT 1)
		RETURNING id,session_id,task_type,args,status,output,error,source,created_at,duration_ms`, sessionID).
		Scan(&t.ID, &t.SessionID, &t.TaskType, &t.Args, &t.Status, &t.Output, &t.Error, &t.Source, &t.CreatedAt, &t.DurationMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &t, err
}
