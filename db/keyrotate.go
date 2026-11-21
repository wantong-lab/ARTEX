package db

// 密钥轮换（P2-13）：把 C2 监听器 token / 代理池 URL / 会话 secret / 凭据 secret
// 从旧 master key 重新加密到新 master key。四个子系统各自使用域分离派生密钥，
// 轮换在同一个数据库事务里完成：任何一行解密失败都整体回滚，不出现「一半旧钥匙
// 一半新钥匙」的半轮换状态。调用方须在服务停止时执行（服务内存在旧 key）。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// RotateCounts 每类密文的轮换行数。
type RotateCounts struct {
	Sessions    int `json:"sessions"`
	Credentials int `json:"credentials"`
	Proxies     int `json:"proxies"`
	C2Listeners int `json:"c2_listeners"`
}

type rotateTarget struct {
	table  string
	column string
	oldKey []byte
	newKey []byte
	count  *int
}

// RotateSecretKeys 用 oldMaster 解、newMaster 重封所有落库密文。dryRun=true 时
// 完整执行校验与重加密但最终回滚（用于上线前预检）。
func RotateSecretKeys(ctx context.Context, d *DB, oldMaster, newMaster []byte, dryRun bool) (RotateCounts, error) {
	var counts RotateCounts
	if len(oldMaster) == 0 || len(newMaster) == 0 {
		return counts, fmt.Errorf("旧/新 master key 不能为空")
	}
	targets := []rotateTarget{
		{table: "sessions", column: "secret", oldKey: SessionSecretKey(oldMaster), newKey: SessionSecretKey(newMaster), count: &counts.Sessions},
		{table: "credentials", column: "secret", oldKey: CredentialSecretKey(oldMaster), newKey: CredentialSecretKey(newMaster), count: &counts.Credentials},
		{table: "proxy_pool", column: "url_enc", oldKey: ProxySecretKey(oldMaster), newKey: ProxySecretKey(newMaster), count: &counts.Proxies},
		{table: "c2_listeners", column: "token_enc", oldKey: C2ListenerSecretKey(oldMaster), newKey: C2ListenerSecretKey(newMaster), count: &counts.C2Listeners},
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return counts, fmt.Errorf("开启轮换事务: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, tg := range targets {
		rows, err := tx.QueryContext(ctx,
			fmt.Sprintf("SELECT id, %s FROM %s WHERE %s <> '' ORDER BY id", tg.column, tg.table, tg.column))
		if err != nil {
			return counts, fmt.Errorf("读取 %s: %w", tg.table, err)
		}
		var updates []struct {
			id  int64
			val string
		}
		for rows.Next() {
			var id int64
			var stored string
			if err := rows.Scan(&id, &stored); err != nil {
				_ = rows.Close()
				return counts, fmt.Errorf("扫描 %s: %w", tg.table, err)
			}
			sealed, err := resealSecret(tg.oldKey, tg.newKey, stored)
			if err != nil {
				_ = rows.Close()
				return counts, fmt.Errorf("%s#%d 解密失败（旧 key 不匹配或数据损坏）: %w", tg.table, id, err)
			}
			updates = append(updates, struct {
				id  int64
				val string
			}{id: id, val: sealed})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return counts, fmt.Errorf("遍历 %s: %w", tg.table, err)
		}
		_ = rows.Close()
		for _, u := range updates {
			if _, err := tx.ExecContext(ctx,
				fmt.Sprintf("UPDATE %s SET %s=$1 WHERE id=$2", tg.table, tg.column), u.val, u.id); err != nil {
				return counts, fmt.Errorf("更新 %s#%d: %w", tg.table, u.id, err)
			}
		}
		*tg.count = len(updates)
	}
	if dryRun {
		_ = tx.Rollback()
		return counts, nil
	}
	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("提交轮换事务: %w", err)
	}
	committed = true
	return counts, nil
}

// resealSecret 用 oldKey 解开 gcm1: 密文、再用 newKey 重新封装。
func resealSecret(oldKey, newKey []byte, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, secretPrefix) {
		return "", fmt.Errorf("密文缺少 %s 前缀（可能是历史明文数据）", secretPrefix)
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(secretPrefix):])
	if err != nil {
		return "", fmt.Errorf("base64 解码: %w", err)
	}
	oldGCM, err := newGCM(oldKey)
	if err != nil {
		return "", err
	}
	if len(raw) < oldGCM.NonceSize() {
		return "", fmt.Errorf("密文过短")
	}
	plain, err := oldGCM.Open(nil, raw[:oldGCM.NonceSize()], raw[oldGCM.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("GCM 校验失败: %w", err)
	}
	newGCM, err := newGCM(newKey)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, newGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(newGCM.Seal(nonce, nonce, plain, nil)), nil
}
