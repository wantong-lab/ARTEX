package server

import (
	"os"
	"testing"
)

// testTempDir 返回一个测试专用临时目录，清理失败只忽略、不让用例变红。
// Server.New 会启动若干后台协程（证据 GC、触发器调度、通知投递、MCP 发现、
// 并发对账等），它们可能在测试函数返回后仍短暂写文件；testTempDir(t) 的清理
// 遇到“目录非空”会直接判失败，导致这批用例随机红（T-03）。
func testTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "artex-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
