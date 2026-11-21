// artex-rotate-key 轮换落库密文 master key（P2-13）。
//
// 用法（先停服务，轮换会重写 sessions/credentials/proxy_pool/c2_listeners 的密文）：
//
//	artex-rotate-key -data ./data -key-dir . -dsn "$ARTEX_PG_DSN" [-dry-run]
//
// 成功后新 key 写入 <key-dir>/jwt.key（0600）。注意：jwt.key 同时用于 JWT 签名，
// 轮换后所有已登录浏览器的 token 失效，需要重新登录。
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/artex/db"
)

func main() {
	dataDir := flag.String("data", filepath.Join(config.BaseDir(), "data"), "data directory (legacy jwt.key fallback)")
	keyDir := flag.String("key-dir", config.BaseDir(), "key directory containing jwt.key")
	dsn := flag.String("dsn", os.Getenv("ARTEX_PG_DSN"), "PostgreSQL DSN (default ARTEX_PG_DSN)")
	dryRun := flag.Bool("dry-run", false, "只校验旧 key 并预演重加密，不提交、不写新 key")
	flag.Parse()
	if strings.TrimSpace(*dsn) == "" {
		log.Fatal("缺少 -dsn 或 ARTEX_PG_DSN")
	}
	keyPath := filepath.Join(*keyDir, "jwt.key")
	oldKey, err := readMasterKey(keyPath)
	if err != nil {
		// 兼容旧位置 data/jwt.key。
		legacy := filepath.Join(*dataDir, "jwt.key")
		if oldKey, err = readMasterKey(legacy); err == nil {
			keyPath = legacy
		}
	}
	if err != nil {
		log.Fatalf("读取 master key 失败（先启动一次服务生成 jwt.key）: %v", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("生成新 key: %v", err)
	}
	newKey := base64.RawURLEncoding.EncodeToString(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, err := db.Open(*dsn)
	if err != nil {
		log.Fatalf("连接数据库: %v", err)
	}
	defer d.Close()
	counts, err := db.RotateSecretKeys(ctx, d, oldKey, []byte(newKey), *dryRun)
	if err != nil {
		log.Fatalf("轮换失败（事务已回滚，旧 key/数据未变）: %v", err)
	}
	if *dryRun {
		out, _ := json.Marshal(map[string]any{"dry_run": true, "counts": counts})
		fmt.Println(string(out))
		return
	}
	tmp := keyPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newKey), 0o600); err != nil {
		log.Fatalf("写新 key 临时文件: %v", err)
	}
	if err := os.Rename(tmp, keyPath); err != nil {
		log.Fatalf("替换 master key（数据库已用新 key 重加密，请手动把 %s 内容更新为新 key）: %v", tmp, err)
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "key_path": keyPath, "counts": counts})
	fmt.Println(string(out))
}

func readMasterKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	k := strings.TrimSpace(string(b))
	if len(k) < 32 {
		return nil, fmt.Errorf("%s 内容长度 %d < 32", path, len(k))
	}
	return []byte(k), nil
}
