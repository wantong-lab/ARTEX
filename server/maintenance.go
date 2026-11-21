package server

// 维护性定时调度（P1-4）：把「保留自动清理 / CVE 自动同步 / 代理池健康巡检」统一到
// 一个后台循环。与 P3 的触发器调度器（newScheduler）互不依赖：这里只做平台自身运维。
// 配置全部来自 settings，运行期每分钟重读一次，改配置无需重启。

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/kb"
)

const (
	settingRetentionAuto     = "retention_auto_enabled"
	settingRetentionInterval = "retention_auto_interval_hours"
	settingCVEInterval       = "cve_autosync_interval_hours"
	settingProxyHealth       = "proxy_health_enabled"
	settingProxyInterval     = "proxy_health_interval_minutes"
)

type maintenanceJob struct {
	name     string
	enabled  func() bool
	interval func() time.Duration
	run      func(ctx context.Context) (string, error)

	next, last time.Time
	lastErr    string
	lastResult string
	running    bool
}

type maintainer struct {
	srv  *Server
	now  func() time.Time
	tick time.Duration
	mu   sync.Mutex
	jobs []*maintenanceJob
}

func newMaintenanceScheduler(s *Server) *maintainer {
	tick := time.Minute
	if v := strings.TrimSpace(os.Getenv("ARTEX_SCHEDULER_TICK_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3600 {
			tick = time.Duration(n) * time.Second
		}
	}
	m := &maintainer{srv: s, now: time.Now, tick: tick}
	m.jobs = []*maintenanceJob{
		{
			name:     "retention",
			enabled:  func() bool { return s.settingBool(settingRetentionAuto, false) },
			interval: func() time.Duration { return s.settingHours(settingRetentionInterval, 24) },
			run:      s.runRetentionJob,
		},
		{
			name:     "cve_autosync",
			enabled:  func() bool { return strings.EqualFold(s.settingRaw("cve_autosync", "off"), "on") },
			interval: func() time.Duration { return s.settingHours(settingCVEInterval, 24) },
			run:      s.runCVEAutosyncJob,
		},
		{
			name:     "proxy_health",
			enabled:  func() bool { return s.settingBool(settingProxyHealth, true) },
			interval: func() time.Duration { return s.settingMinutes(settingProxyInterval, 30) },
			run:      s.runProxyHealthJob,
		},
	}
	return m
}

// Run 阻塞到 ctx 取消；每 tick 检查一次到期任务。
func (m *maintainer) Run(ctx context.Context) {
	t := time.NewTicker(m.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.tickOnce(now)
		}
	}
}

// tickOnce 是调度核心（独立可测）：首次见到启用的任务只排期；到期后异步执行并顺延。
func (m *maintainer) tickOnce(now time.Time) {
	for _, j := range m.jobs {
		if !j.enabled() {
			continue
		}
		m.mu.Lock()
		if j.next.IsZero() {
			j.next = now.Add(j.interval())
			m.mu.Unlock()
			continue
		}
		if j.running || now.Before(j.next) {
			m.mu.Unlock()
			continue
		}
		j.running = true
		j.last = now
		j.next = now.Add(j.interval())
		m.mu.Unlock()
		go m.exec(j)
	}
}

func (m *maintainer) exec(j *maintenanceJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] %s panic: %v", j.name, r)
		}
	}()
	res, err := j.run(ctx)
	m.mu.Lock()
	j.running = false
	j.lastResult = res
	if err != nil {
		j.lastErr = err.Error()
		m.mu.Unlock()
		log.Printf("[scheduler] %s 失败: %v", j.name, err)
		return
	}
	j.lastErr = ""
	m.mu.Unlock()
	if res != "" {
		log.Printf("[scheduler] %s: %s", j.name, res)
	}
}

// RunNow 手动执行一个任务（运维手动触发/验收用），同步返回结果。
func (m *maintainer) RunNow(ctx context.Context, name string) (string, error) {
	for _, j := range m.jobs {
		if j.name != name {
			continue
		}
		m.mu.Lock()
		if j.running {
			m.mu.Unlock()
			return "", fmt.Errorf("任务 %s 正在运行", name)
		}
		j.running = true
		j.last = m.now()
		m.mu.Unlock()
		res, err := j.run(ctx)
		m.mu.Lock()
		j.running = false
		j.lastResult = res
		if err != nil {
			j.lastErr = err.Error()
		} else {
			j.lastErr = ""
		}
		m.mu.Unlock()
		return res, err
	}
	return "", fmt.Errorf("未知调度任务 %q", name)
}

// Status 返回给 /api/scheduler/status 的快照。
func (m *maintainer) Status() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, 0, len(m.jobs))
	for _, j := range m.jobs {
		item := map[string]any{
			"name":             j.name,
			"enabled":          j.enabled(),
			"running":          j.running,
			"interval_seconds": int(j.interval().Seconds()),
			"last_error":       j.lastErr,
			"last_result":      j.lastResult,
		}
		if !j.next.IsZero() {
			item["next_run_unix"] = j.next.Unix()
		}
		if !j.last.IsZero() {
			item["last_run_unix"] = j.last.Unix()
		}
		out = append(out, item)
	}
	return out
}

// ---- settings 读取（运行期热更新） ----

func (s *Server) settingRaw(key, def string) string {
	if s.m == nil || s.m.pg == nil {
		return def
	}
	v, ok, err := s.m.pg.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func (s *Server) settingBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(s.settingRaw(key, "")))
	if v == "" {
		return def
	}
	switch v {
	case "on", "true", "1", "yes":
		return true
	case "off", "false", "0", "no":
		return false
	}
	return def
}

func (s *Server) settingInt(key string, def, minV, maxV int) int {
	raw := s.settingRaw(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minV || v > maxV {
		return def
	}
	return v
}

func (s *Server) settingHours(key string, defHours int) time.Duration {
	return time.Duration(s.settingInt(key, defHours, 1, 168)) * time.Hour
}

func (s *Server) settingMinutes(key string, defMinutes int) time.Duration {
	return time.Duration(s.settingInt(key, defMinutes, 5, 1440)) * time.Minute
}

// ---- 实际任务 ----

func (s *Server) runRetentionJob(ctx context.Context) (string, error) {
	if s.m == nil || s.m.pg == nil {
		return "", fmt.Errorf("数据库未就绪")
	}
	policy := s.m.pg.LoadRetentionPolicy()
	res, err := s.m.pg.RunRetentionCleanup(ctx, policy, "scheduler", false)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted activity=%d logs=%d c2_tasks=%d c2_events=%d c2_sessions=%d audit=%d",
		res.Deleted.Activity, res.Deleted.ServerLogs, res.Deleted.C2Tasks, res.Deleted.C2Events, res.Deleted.C2Sessions, res.AuditID), nil
}

func (s *Server) runCVEAutosyncJob(ctx context.Context) (string, error) {
	if s.m == nil {
		return "", fmt.Errorf("管理器未就绪")
	}
	dir := filepath.Join(s.m.dir, "cve")
	_ = os.MkdirAll(dir, 0o755)
	interval := s.settingHours(settingCVEInterval, 24)
	st := kb.LoadCVESyncState(dir)
	if s.cveClient() != nil && st.Done && !st.UpdatedAt.IsZero() && time.Since(st.UpdatedAt) < interval {
		return "语料库在有效期内，跳过同步", nil
	}
	if s.launchCVESync("") {
		return "已触发 CVE 同步", nil
	}
	return "CVE 同步已在运行", nil
}

func (s *Server) runProxyHealthJob(ctx context.Context) (string, error) {
	rt := s.proxyPool()
	if rt == nil {
		return "", fmt.Errorf("代理池不可用")
	}
	entries, err := rt.dbStore.List(ctx)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "代理池为空，跳过巡检", nil
	}
	okCnt, failCnt := 0, 0
	for _, e := range entries {
		res, perr := rt.mgr.Probe(ctx, e.ID)
		if perr != nil || !res.OK {
			failCnt++
			continue
		}
		okCnt++
	}
	return fmt.Sprintf("巡检 %d 条：ok=%d fail=%d", len(entries), okCnt, failCnt), nil
}

func (s *Server) schedulerRun(w http.ResponseWriter, r *http.Request) {
	if s.maintenance == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "调度器未启动"})
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.maintenance.RunNow(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "result": res})
}

func (s *Server) schedulerStatus(w http.ResponseWriter, r *http.Request) {
	if s.maintenance == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "调度器未启动"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tick_seconds": int(s.maintenance.tick.Seconds()),
		"now_unix":     time.Now().Unix(),
		"jobs":         s.maintenance.Status(),
	})
}
