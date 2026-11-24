package db

// 出口控制面 P0：受管订阅源。配置/最近成功 YAML 加密落库；proxy_pool 继续作为 endpoint。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type EgressSource struct {
	ID             int64
	Name           string
	Kind           string
	DesiredState   string
	ObservedState  string
	ConfigURL      string
	ConfigHash     string
	LastGoodConfig string
	LastGoodHash   string
	ManagedProxyID int64
	ManagedPort    int
	SelectedNode   string
	SelectionMode  string
	ETag           string
	LastModified   string
	LastFetchAt    time.Time
	LastError      string
	Revision       int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type EgressSourceStore struct {
	d   *DB
	key []byte
}

func NewEgressSourceStore(d *DB, master []byte) *EgressSourceStore {
	var key []byte
	if len(master) > 0 {
		sum := sha256.Sum256(append([]byte("artex/egress-sources/v1\x00"), master...))
		key = sum[:]
	}
	return &EgressSourceStore{d: d, key: key}
}

func (s *EgressSourceStore) seal(plain string) (string, error) {
	if len(s.key) == 0 {
		return "", fmt.Errorf("出口源密钥未初始化")
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

func (s *EgressSourceStore) open(blob string) (string, error) {
	if strings.TrimSpace(blob) == "" {
		return "", nil
	}
	if len(s.key) == 0 {
		return "", fmt.Errorf("出口源密钥未初始化")
	}
	if !strings.HasPrefix(blob, secretPrefix) {
		return "", fmt.Errorf("出口源密文格式非法")
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
		return "", fmt.Errorf("出口源密文过短")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func hashText(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

const egressSourceCols = "id,name,kind,desired_state,observed_state,config_enc,config_hash,last_good_config_enc,last_good_hash,managed_proxy_id,managed_port,etag,last_modified,last_fetch_at,last_error,revision,created_at,updated_at,selected_node,selection_mode"

func (s *EgressSourceStore) scan(row interface{ Scan(...any) error }) (*EgressSource, error) {
	var e EgressSource
	var cfgEnc, goodEnc string
	var managed sql.NullInt64
	var lastFetch sql.NullTime
	if err := row.Scan(&e.ID, &e.Name, &e.Kind, &e.DesiredState, &e.ObservedState,
		&cfgEnc, &e.ConfigHash, &goodEnc, &e.LastGoodHash,
		&managed, &e.ManagedPort, &e.ETag, &e.LastModified,
		&lastFetch, &e.LastError, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &e.SelectedNode, &e.SelectionMode); err != nil {
		return nil, err
	}
	var err error
	if e.ConfigURL, err = s.open(cfgEnc); err != nil {
		return nil, err
	}
	if e.LastGoodConfig, err = s.open(goodEnc); err != nil {
		return nil, err
	}
	if managed.Valid {
		e.ManagedProxyID = managed.Int64
	}
	if lastFetch.Valid {
		e.LastFetchAt = lastFetch.Time
	}
	return &e, nil
}

func (s *EgressSourceStore) GetSubscription(ctx context.Context) (*EgressSource, error) {
	row := s.d.QueryRowContext(ctx, "SELECT "+egressSourceCols+" FROM egress_sources WHERE kind='subscription' ORDER BY id LIMIT 1")
	e, err := s.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

func (s *EgressSourceStore) UpsertSubscription(ctx context.Context, name, rawURL string) (*EgressSource, error) {
	enc, err := s.seal(rawURL)
	if err != nil {
		return nil, err
	}
	h := hashText(rawURL)
	if _, err := s.d.ExecContext(ctx, "INSERT INTO egress_sources(name,kind,desired_state,observed_state,config_enc,config_hash) VALUES($1,'subscription','active','starting',$2,$3) ON CONFLICT (kind) DO UPDATE SET name=EXCLUDED.name, desired_state='active', observed_state='starting', config_enc=EXCLUDED.config_enc, config_hash=EXCLUDED.config_hash, last_error='', revision=egress_sources.revision+1, updated_at=now()", name, enc, h); err != nil {
		return nil, err
	}
	return s.GetSubscription(ctx)
}

func (s *EgressSourceStore) SetDesired(ctx context.Context, id int64, state string) error {
	_, err := s.d.ExecContext(ctx, "UPDATE egress_sources SET desired_state=$2, revision=revision+1, updated_at=now() WHERE id=$1", id, state)
	return err
}

func (s *EgressSourceStore) UpdateObserved(ctx context.Context, id int64, state, lastErr string, proxyID int64, port int) error {
	var managed sql.NullInt64
	if proxyID > 0 {
		managed.Valid = true
		managed.Int64 = proxyID
	}
	_, err := s.d.ExecContext(ctx, "UPDATE egress_sources SET observed_state=$2,last_error=$3,managed_proxy_id=$4,managed_port=$5,revision=revision+1,updated_at=now() WHERE id=$1", id, state, lastErr, managed, port)
	return err
}

func (s *EgressSourceStore) SaveLastGood(ctx context.Context, id int64, cfg string) error {
	enc, err := s.seal(cfg)
	if err != nil {
		return err
	}
	_, err = s.d.ExecContext(ctx, "UPDATE egress_sources SET last_good_config_enc=$2,last_good_hash=$3,last_fetch_at=now(),revision=revision+1,updated_at=now() WHERE id=$1", id, enc, hashText(cfg))
	return err
}

func (s *EgressSourceStore) UpdateSelection(ctx context.Context, id int64, mode, node string) error {
	_, err := s.d.ExecContext(ctx, "UPDATE egress_sources SET selection_mode=$2,selected_node=$3,revision=revision+1,updated_at=now() WHERE id=$1", id, mode, node)
	return err
}

func (s *EgressSourceStore) Delete(ctx context.Context, id int64) error {
	_, err := s.d.ExecContext(ctx, "DELETE FROM egress_sources WHERE id=$1", id)
	return err
}
