package db

import (
	"encoding/json"
	"testing"
)

// TestRotateSecretKeys 验证四个子系统（会话/凭据/代理池/C2 监听器）的密文
// 能从旧 master key 原子轮换到新 key；dry-run 不落库；错误旧 key 整体回滚。
func TestRotateSecretKeys(t *testing.T) {
	d := openC2TestDB(t)
	ctx := t.Context()
	oldMaster := []byte("old-master-key-0123456789abcdef-0001")
	newMaster := []byte("new-master-key-0123456789abcdef-0002")

	ssOld := NewSessionStore(d, oldMaster)
	sid, err := ssOld.Create(ctx, &SessionRecord{Kind: "http_phpcmd", URL: "http://127.0.0.1/shell.php", Secret: json.RawMessage("session-secret-1"), Lang: "phpcmd", CreatedByTask: 1})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	csOld := NewCredentialStore(d, oldMaster)
	cid, err := csOld.Create(ctx, &CredentialRecord{TaskID: 1, Username: "u", CredType: CredPassword, Secret: "cred-secret-1", Source: "test"})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	psOld := NewProxyStore(d, oldMaster)
	pid, _, err := psOld.Create(ctx, &ProxyEntry{Name: "rot", URL: "socks5://user:pass@127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	c2Old := NewC2Store(d, oldMaster)
	lid, err := c2Old.CreateListener(ctx, &C2Listener{Name: "rot", Kind: "tcp", BindHost: "127.0.0.1", BindPort: 45123, Token: "c2-token-1"})
	if err != nil {
		t.Fatalf("create c2 listener: %v", err)
	}

	// dry-run：完整预演后回滚，数据库仍可用旧 key 读取。
	counts, err := RotateSecretKeys(ctx, d, oldMaster, newMaster, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if counts.Sessions != 1 || counts.Credentials != 1 || counts.Proxies != 1 || counts.C2Listeners != 1 {
		t.Fatalf("dry-run counts=%+v", counts)
	}
	if rec, err := ssOld.Get(ctx, sid); err != nil || string(rec.Secret) != "session-secret-1" {
		t.Fatalf("dry-run must not change data: rec=%+v err=%v", rec, err)
	}

	// 真正轮换。
	counts, err = RotateSecretKeys(ctx, d, oldMaster, newMaster, false)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if counts.Sessions != 1 || counts.Credentials != 1 || counts.Proxies != 1 || counts.C2Listeners != 1 {
		t.Fatalf("counts=%+v", counts)
	}

	ssNew := NewSessionStore(d, newMaster)
	if rec, err := ssNew.Get(ctx, sid); err != nil || string(rec.Secret) != "session-secret-1" {
		t.Fatalf("session not readable with new key: rec=%+v err=%v", rec, err)
	}
	csNew := NewCredentialStore(d, newMaster)
	if rec, err := csNew.Get(ctx, cid); err != nil || rec.Secret != "cred-secret-1" || !rec.SecretOK {
		t.Fatalf("credential not readable with new key: rec=%+v err=%v", rec, err)
	}
	psNew := NewProxyStore(d, newMaster)
	if rec, err := psNew.Get(ctx, pid); err != nil || rec.URL != "socks5://user:pass@127.0.0.1:18080" {
		t.Fatalf("proxy not readable with new key: rec=%+v err=%v", rec, err)
	}
	c2New := NewC2Store(d, newMaster)
	if rec, err := c2New.GetListener(ctx, lid); err != nil || rec.Token != "c2-token-1" {
		t.Fatalf("c2 token not readable with new key: rec=%+v err=%v", rec, err)
	}

	// 旧 key 已解不开（诚实失败，不返回明文）。
	if _, err := ssOld.Get(ctx, sid); err == nil {
		t.Fatal("old key must fail to decrypt rotated session secret")
	}
	if rec, err := csOld.Get(ctx, cid); err == nil && rec.SecretOK {
		t.Fatal("old key must fail to decrypt rotated credential secret")
	}
	// 代理池读路径对解密失败是降级（只展示 host:port，不返回明文 URL），
	// 所以旧 key 下 URL 必须为空而不是报错。
	if rec, err := psOld.Get(ctx, pid); err != nil || rec.URL != "" {
		t.Fatalf("old key must not decrypt proxy url: rec=%+v err=%v", rec, err)
	}
	// C2 监听器列表同样降级展示（token 留空），不因单行解密失败报错。
	if rec, err := c2Old.GetListener(ctx, lid); err != nil || rec.Token != "" {
		t.Fatalf("old key must not decrypt c2 token: rec=%+v err=%v", rec, err)
	}

	// 错误旧 key：事务回滚，数据仍是新 key 可解。
	if _, err := RotateSecretKeys(ctx, d, []byte("wrong-old-master-key-0123456789abcdef"), []byte("third-master-key-0123456789abcdef-03"), false); err == nil {
		t.Fatal("rotation with wrong old key must fail")
	}
	if rec, err := ssNew.Get(ctx, sid); err != nil || string(rec.Secret) != "session-secret-1" {
		t.Fatalf("failed rotation must roll back: rec=%+v err=%v", rec, err)
	}

	// 空 key 拒绝。
	if _, err := RotateSecretKeys(ctx, d, nil, newMaster, false); err == nil {
		t.Fatal("empty old master key must be rejected")
	}
}
