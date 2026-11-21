package db

// 代理池台账（M4-03）：入口 URL 加密存储，对外只暴露 host:port 与健康状态。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	ProxyActive   = "active"
	ProxyDisabled = "disabled"
	ProxyFailed   = "failed"
)

type ProxyEntry struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"-"` // 明文仅内存：含认证信息
	Scheme      string    `json:"scheme"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Status      string    `json:"status"`
	ExitIP      string    `json:"exit_ip"`
	LatencyMs   int       `json:"latency_ms"`
	FailCount   int       `json:"fail_count"`
	LastChecked time.Time `json:"last_checked,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Masked 返回可公开展示的代理地址（密码打码）。
func (e *ProxyEntry) Masked() string {
	if e == nil {
		return ""
	}
	if e.Host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s:%d", e.Scheme, e.Host, e.Port)
}

type ProxyStore struct {
	d   *DB
	key []byte
}

// ProxySecretKey 由 master key 派生代理池加密密钥（SHA-256 域分离）。
func ProxySecretKey(master []byte) []byte {
	sum := sha256.Sum256(append([]byte("artex/proxypool/v1\x00"), master...))
	return sum[:]
}

func NewProxyStore(d *DB, masterKey []byte) *ProxyStore {
	var key []byte
	if len(masterKey) > 0 {
		key = ProxySecretKey(masterKey)
	}
	return &ProxyStore{d: d, key: key}
}

func (s *ProxyStore) seal(plain string) (string, error) {
	if len(s.key) == 0 {
		return "", fmt.Errorf("代理池密钥未初始化")
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

func (s *ProxyStore) open(blob string) (string, error) {
	if len(s.key) == 0 {
		return "", fmt.Errorf("代理池密钥未初始化")
	}
	if !strings.HasPrefix(blob, secretPrefix) {
		return "", fmt.Errorf("代理 URL 密文格式非法")
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
		return "", fmt.Errorf("代理 URL 密文过短")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Create 录入一个代理；同 scheme+host+port 视为重复（返回已存在行 id 与 dup=true）。
func (s *ProxyStore) Create(ctx context.Context, e *ProxyEntry) (int64, bool, error) {
	u, err := url.Parse(strings.TrimSpace(e.URL))
	if err != nil || u.Host == "" {
		return 0, false, fmt.Errorf("代理 URL 非法")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return 0, false, fmt.Errorf("不支持的代理协议 %q（支持 http/https/socks5/socks5h）", scheme)
	}
	port := 0
	if p := u.Port(); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}
	if port <= 0 || port > 65535 {
		return 0, false, fmt.Errorf("代理端口非法")
	}
	enc, err := s.seal(e.URL)
	if err != nil {
		return 0, false, err
	}
	var id int64
	err = s.d.QueryRowContext(ctx, `INSERT INTO proxy_pool(name,url_enc,scheme,host,port,status)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(scheme,host,port) DO NOTHING RETURNING id`,
		e.Name, enc, scheme, u.Hostname(), port, ProxyActive).Scan(&id)
	if err == sql.ErrNoRows {
		// 已存在：返回原行 id
		err = s.d.QueryRowContext(ctx, `SELECT id FROM proxy_pool WHERE scheme=$1 AND host=$2 AND port=$3`,
			scheme, u.Hostname(), port).Scan(&id)
		return id, true, err
	}
	return id, false, err
}

const proxyCols = `id,name,url_enc,scheme,host,port,status,exit_ip,latency_ms,fail_count,last_checked,last_error,created_at`

func scanProxyEntry(row interface{ Scan(...any) error }, s *ProxyStore) (*ProxyEntry, error) {
	var e ProxyEntry
	var enc string
	var checked sql.NullTime
	if err := row.Scan(&e.ID, &e.Name, &enc, &e.Scheme, &e.Host, &e.Port, &e.Status, &e.ExitIP, &e.LatencyMs, &e.FailCount, &checked, &e.LastError, &e.CreatedAt); err != nil {
		return nil, err
	}
	if checked.Valid {
		e.LastChecked = checked.Time
	}
	if plain, err := s.open(enc); err == nil {
		e.URL = plain
	}
	return &e, nil
}

func (s *ProxyStore) List(ctx context.Context) ([]*ProxyEntry, error) {
	rows, err := s.d.QueryContext(ctx, `SELECT `+proxyCols+` FROM proxy_pool ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProxyEntry
	for rows.Next() {
		e, err := scanProxyEntry(rows, s)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *ProxyStore) Get(ctx context.Context, id int64) (*ProxyEntry, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+proxyCols+` FROM proxy_pool WHERE id=$1`, id)
	e, err := scanProxyEntry(row, s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

// UpdateProbe 记录一次探测结果：成功置 active 并清零失败数，失败累计 fail_count。
func (s *ProxyStore) UpdateProbe(ctx context.Context, id int64, exitIP string, latencyMs int, errMsg string) error {
	if strings.TrimSpace(errMsg) == "" {
		_, err := s.d.ExecContext(ctx, `UPDATE proxy_pool SET status=$2,exit_ip=$3,latency_ms=$4,fail_count=0,
			last_checked=now(),last_error='',updated_at=now() WHERE id=$1`,
			id, ProxyActive, exitIP, latencyMs)
		return err
	}
	_, err := s.d.ExecContext(ctx, `UPDATE proxy_pool SET status=$2,fail_count=fail_count+1,
		last_checked=now(),last_error=$3,updated_at=now() WHERE id=$1`, id, ProxyFailed, errMsg)
	return err
}

// RecordFailure 在真实请求失败时累计失败（不改变 exit_ip/latency）。
func (s *ProxyStore) RecordFailure(ctx context.Context, id int64, errMsg string) error {
	_, err := s.d.ExecContext(ctx, `UPDATE proxy_pool SET status=$2,fail_count=fail_count+1,last_error=$3,updated_at=now() WHERE id=$1`,
		id, ProxyFailed, errMsg)
	return err
}

func (s *ProxyStore) SetStatus(ctx context.Context, id int64, status string) error {
	_, err := s.d.ExecContext(ctx, `UPDATE proxy_pool SET status=$2,updated_at=now() WHERE id=$1`, id, status)
	return err
}

func (s *ProxyStore) Delete(ctx context.Context, id int64) error {
	_, err := s.d.ExecContext(ctx, `DELETE FROM proxy_pool WHERE id=$1`, id)
	return err
}
