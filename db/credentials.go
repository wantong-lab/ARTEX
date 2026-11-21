// credentials.go 是凭据一等实体的落库层(schema.sql §K,内网渗透期 2,
// INTRANET-PIVOT-DESIGN.md §4.4)。secret 用 AES-GCM 加密存储:与 sessions 同款
// 派生方式(jwt.key → SHA-256 域分离)但不同域标签,两个子系统密钥互相独立。
package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

// 凭据类型全集(schema CHECK 与 Go 校验同源)。
const (
	CredPassword = "password"
	CredHashNT   = "hash_nt"
	CredHashLM   = "hash_lm"
	CredHashSHA1 = "hash_sha1"
	CredTicket   = "ticket"
	CredSSHKey   = "ssh_key"
	CredToken    = "token"
	CredHashUnix = "hash_unix" // /etc/shadow 系 crypt 哈希($1$/$5$/$6$/$y$ 等)
)

// CredentialRecord 是 credentials 表的一行。Secret 在 Go 层是明文;Create 加密、
// Get/ListByTask 解密,密钥不出 CredentialStore。
type CredentialRecord struct {
	ID          int64     `json:"id"`
	TaskID      int64     `json:"task_id"`
	HostAssetID int64     `json:"host_asset_id,omitempty"`
	Username    string    `json:"username"`
	CredType    string    `json:"cred_type"`
	Secret      string    `json:"secret"` // 明文;落库时加密
	Domain      string    `json:"domain,omitempty"`
	Source      string    `json:"source"`
	Verified    bool      `json:"verified"`
	CreatedAt   time.Time `json:"created_at"`
	// SecretOK 表示 secret 能否用当前 master key 解密。迁移/换 key 后旧密文会
	// 解不开;此时保留记录并标记原因,不能因为一行坏数据让整个凭据台账 500。
	SecretOK    bool   `json:"secret_ok"`
	SecretError string `json:"secret_error,omitempty"`
}

// ValidCredType 校验凭据类型。
func ValidCredType(t string) bool {
	switch t {
	case CredPassword, CredHashNT, CredHashLM, CredHashSHA1, CredTicket, CredSSHKey, CredToken, CredHashUnix:
		return true
	}
	return false
}

// CredentialSecretKey 由 master key(jwt.key 内容)派生凭据加密密钥:与
// SessionSecretKey 同款 SHA-256 域分离,但域标签不同("artex/credentials/v1"),
// 两个子系统的密文互不通用。
func CredentialSecretKey(master []byte) []byte {
	sum := sha256.Sum256(append([]byte("artex/credentials/v1\x00"), master...))
	return sum[:]
}

// CredentialStore 是 credentials 表的访问层。key 为 32 字节 AES 密钥(经
// CredentialSecretKey 派生);key 缺失时加解密直接报错(诚实失败,不落明文)。
type CredentialStore struct {
	d   *DB
	key []byte
}

// NewCredentialStore 构造访问层。masterKey 传 jwt.key 内容;内部自行派生。
func NewCredentialStore(d *DB, masterKey []byte) *CredentialStore {
	var key []byte
	if len(masterKey) > 0 {
		key = CredentialSecretKey(masterKey)
	}
	return &CredentialStore{d: d, key: key}
}

func (s *CredentialStore) encrypt(plain string) (string, error) {
	if len(s.key) != 32 {
		return "", fmt.Errorf("凭据加密密钥未初始化,拒绝明文落库")
	}
	gcm, err := newGCM(s.key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *CredentialStore) decrypt(stored string) (string, error) {
	if len(stored) == 0 {
		return "", nil
	}
	if len(s.key) != 32 {
		return "", fmt.Errorf("凭据加密密钥未初始化,无法解密")
	}
	if !hasPrefix(stored, secretPrefix) {
		return "", fmt.Errorf("未知 secret 格式(缺 gcm1: 前缀),拒绝按明文读取")
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(secretPrefix):])
	if err != nil {
		return "", fmt.Errorf("secret base64 解码失败: %w", err)
	}
	gcm, err := newGCM(s.key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("secret 密文过短")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("secret 解密失败(密钥不符或数据被篡改): %w", err)
	}
	return string(plain), nil
}

// Create 插入一条凭据并返回主键。Secret 加密存储。
func (s *CredentialStore) Create(ctx context.Context, rec *CredentialRecord) (int64, error) {
	if rec.TaskID <= 0 {
		return 0, fmt.Errorf("task_id 必填")
	}
	if !ValidCredType(rec.CredType) {
		return 0, fmt.Errorf("非法 cred_type %q(password/hash_nt/hash_lm/hash_sha1/ticket/ssh_key/token)", rec.CredType)
	}
	if rec.Secret == "" {
		return 0, fmt.Errorf("secret 必填")
	}
	enc, err := s.encrypt(rec.Secret)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.d.QueryRowContext(ctx, `
INSERT INTO credentials(task_id, host_asset_id, username, cred_type, secret, domain, source, verified)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`,
		rec.TaskID, nilIfZero(rec.HostAssetID), rec.Username, rec.CredType, enc,
		rec.Domain, rec.Source, rec.Verified).Scan(&id)
	return id, err
}

const credentialCols = `id, task_id, COALESCE(host_asset_id,0), username, cred_type, secret,
	domain, source, verified, created_at`

func (s *CredentialStore) scan(row interface{ Scan(...any) error }) (*CredentialRecord, error) {
	var r CredentialRecord
	var enc string
	if err := row.Scan(&r.ID, &r.TaskID, &r.HostAssetID, &r.Username, &r.CredType, &enc,
		&r.Domain, &r.Source, &r.Verified, &r.CreatedAt); err != nil {
		return nil, err
	}
	plain, derr := s.decrypt(enc)
	if derr != nil {
		// 旧 master key(jwt.key 轮换/跨机迁移)加密的历史密文解不开。降级为
		// 「可见但不可解密」:台账仍能列出全部凭据,单条标 secret_ok=false,
		// 页面/导出据此提示,而不是整页 500 → 空列表。
		r.SecretOK = false
		r.SecretError = derr.Error()
		return &r, nil
	}
	r.SecretOK = true
	r.Secret = plain
	return &r, nil
}

// Get 取一条(含解密 secret);不存在返回 (nil, nil)。
func (s *CredentialStore) Get(ctx context.Context, id int64) (*CredentialRecord, error) {
	r, err := s.scan(s.d.QueryRowContext(ctx,
		`SELECT `+credentialCols+` FROM credentials WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// ListByTask 取一个任务的全部凭据(按 id 升序,含解密 secret)。
func (s *CredentialStore) ListByTask(ctx context.Context, taskID int64) ([]*CredentialRecord, error) {
	rows, err := s.d.QueryContext(ctx,
		`SELECT `+credentialCols+` FROM credentials WHERE task_id=$1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CredentialRecord{}
	for rows.Next() {
		r, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAll 取全部凭据(按 id 升序,含解密 secret)——内网作战页跨任务视图用。
// 调用方负责脱敏(MaskSecret),secret 不明文出 server。
func (s *CredentialStore) ListAll(ctx context.Context) ([]*CredentialRecord, error) {
	rows, err := s.d.QueryContext(ctx,
		`SELECT `+credentialCols+` FROM credentials ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CredentialRecord{}
	for rows.Next() {
		r, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CredentialStats 是 CredentialStatsByTask 的返回结构。
type CredentialStats struct {
	Total      int            `json:"total"`
	Verified   int            `json:"verified"`
	Unverified int            `json:"unverified"`
	ByType     map[string]int `json:"by_type"` // cred_type → count
}

// CredentialStatsByTask 返回一个任务的凭据统计(不解密 secret)。
func (s *CredentialStore) CredentialStatsByTask(ctx context.Context, taskID int64) (*CredentialStats, error) {
	rows, err := s.d.QueryContext(ctx,
		`SELECT cred_type, verified, count(*) FROM credentials WHERE task_id=$1 GROUP BY cred_type, verified`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st := &CredentialStats{ByType: map[string]int{}}
	for rows.Next() {
		var ct string
		var v bool
		var cnt int
		if err := rows.Scan(&ct, &v, &cnt); err != nil {
			return nil, err
		}
		st.Total += cnt
		st.ByType[ct] += cnt
		if v {
			st.Verified += cnt
		} else {
			st.Unverified += cnt
		}
	}
	return st, rows.Err()
}

// SetVerified 标记凭据是否已实测可登录(复用打分的 verified 加分依赖它)。
func (s *CredentialStore) SetVerified(ctx context.Context, id int64, verified bool) error {
	_, err := s.d.ExecContext(ctx, `UPDATE credentials SET verified=$2 WHERE id=$1`, id, verified)
	return err
}

// Delete 删除一条。
func (s *CredentialStore) Delete(ctx context.Context, id int64) error {
	_, err := s.d.ExecContext(ctx, `DELETE FROM credentials WHERE id=$1`, id)
	return err
}

// MaskSecret 脱敏展示:只露前后各 2 字符,其余掩码;过短(≤4 字符)全掩码。
func MaskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}
