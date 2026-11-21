package agent

// 运行时钩子必须在并发下安全：engine 启动后仍可能热更新提示词/工具可见性，
// 而 planner/worker 会同时读取这些钩子。早年它们直接暴露为可变包级变量，
// race 检测在「编辑提示词 + 正在跑任务」时能稳定复现数据竞争。
// 这里统一用 RWMutex 保护，写入走 Set*，读取走内部 get*（测试可用公开 Get* 保存/恢复）。

import (
	"context"
	"sync"

	actool "github.com/Autumn-27/norma/tool"
)

var hookMu sync.RWMutex

var (
	toolAugmentHook           func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func())
	toolResolveHook           func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool
	promptOverrideHook        func(agentKey string) (string, bool)
	findingTrafficBindingHook func() bool
	wrapupOverrideHook        func(agentKey string) (string, bool)
	wrapupMaxTurnsHook        func(agentKey string) (int, bool)
)

// SetToolAugment installs the tool-augmentation hook (nil clears it).
func SetToolAugment(fn func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func())) {
	hookMu.Lock()
	toolAugmentHook = fn
	hookMu.Unlock()
}

// GetToolAugment returns the current tool-augmentation hook (tests save/restore it).
func GetToolAugment() func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func()) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return toolAugmentHook
}

func getToolAugment() func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func()) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return toolAugmentHook
}

// SetToolResolve installs the DB tool-resolution hook (nil clears it).
func SetToolResolve(fn func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool) {
	hookMu.Lock()
	toolResolveHook = fn
	hookMu.Unlock()
}

// GetToolResolve returns the current tool-resolution hook.
func GetToolResolve() func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return toolResolveHook
}

func getToolResolve() func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return toolResolveHook
}

// SetPromptOverride installs the system-prompt override hook (nil clears it).
func SetPromptOverride(fn func(agentKey string) (string, bool)) {
	hookMu.Lock()
	promptOverrideHook = fn
	hookMu.Unlock()
}

// GetPromptOverride returns the current prompt-override hook.
func GetPromptOverride() func(agentKey string) (string, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return promptOverrideHook
}

func getPromptOverride() func(agentKey string) (string, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return promptOverrideHook
}

// SetFindingTrafficBindingEnabled installs the finding/traffic binding policy hook.
func SetFindingTrafficBindingEnabled(fn func() bool) {
	hookMu.Lock()
	findingTrafficBindingHook = fn
	hookMu.Unlock()
}

// GetFindingTrafficBindingEnabled returns the current binding policy hook.
func GetFindingTrafficBindingEnabled() func() bool {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return findingTrafficBindingHook
}

func getFindingTrafficBindingEnabled() func() bool {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return findingTrafficBindingHook
}

// SetWrapupOverride installs the wrap-up prompt hook.
func SetWrapupOverride(fn func(agentKey string) (string, bool)) {
	hookMu.Lock()
	wrapupOverrideHook = fn
	hookMu.Unlock()
}

// GetWrapupOverride returns the current wrap-up prompt hook.
func GetWrapupOverride() func(agentKey string) (string, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return wrapupOverrideHook
}

func getWrapupOverride() func(agentKey string) (string, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return wrapupOverrideHook
}

// SetWrapupMaxTurnsOverride installs the wrap-up turn-budget hook.
func SetWrapupMaxTurnsOverride(fn func(agentKey string) (int, bool)) {
	hookMu.Lock()
	wrapupMaxTurnsHook = fn
	hookMu.Unlock()
}

// GetWrapupMaxTurnsOverride returns the current wrap-up turn-budget hook.
func GetWrapupMaxTurnsOverride() func(agentKey string) (int, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return wrapupMaxTurnsHook
}

func getWrapupMaxTurnsOverride() func(agentKey string) (int, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return wrapupMaxTurnsHook
}
