package server

// P2-14 可观测性：/metrics（Prometheus 文本）、结构化访问日志、清理/同步调度指标，
// 以及 GET /api/observability/status 汇总。全部手写无第三方依赖。

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
)

// httpMetrics 是进程内 HTTP 指标（无第三方依赖）。key 使用 method 与 code。
type httpMetrics struct {
	mu          sync.Mutex
	inFlight    int64
	requests    map[string]int64
	durSum      map[string]float64
	durCount    map[string]int64
	bytesByCode map[string]int64
}

func newHTTPMetrics() *httpMetrics {
	return &httpMetrics{
		requests:    map[string]int64{},
		durSum:      map[string]float64{},
		durCount:    map[string]int64{},
		bytesByCode: map[string]int64{},
	}
}

func (m *httpMetrics) begin() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.inFlight++
	m.mu.Unlock()
}

func (m *httpMetrics) finish(method string, code int, dur time.Duration, bytes int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inFlight--
	key := method + "|" + strconv.Itoa(code)
	m.requests[key]++
	m.durSum[method] += dur.Seconds()
	m.durCount[method]++
	m.bytesByCode[strconv.Itoa(code)] += int64(bytes)
}

// obsRecorder 记录状态码与响应字节数；必须透传 Flush，否则日志/活动 SSE 流会被
// 缓冲住（这是包装 ResponseWriter 最容易踩的坑）。
type obsRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *obsRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *obsRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *obsRecorder) Flush() {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// metricsMiddleware 统计所有经过它的请求，并在 JSON 日志模式（或显式开启访问日志）
// 下输出一条结构化访问日志。不记录 query（避免 token 泄漏进日志）。
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.metrics.begin()
		rec := &obsRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		code := rec.status
		if code == 0 {
			code = http.StatusOK
		}
		dur := time.Since(start)
		s.metrics.finish(r.Method, code, dur, rec.bytes)
		if obsJSONEnabled() || os.Getenv("ARTEX_ACCESS_LOG") == "1" {
			s.obsLog("info", "http_request", map[string]any{
				"method": r.Method, "path": r.URL.Path, "status": code,
				"duration_ms": dur.Milliseconds(), "bytes": rec.bytes, "remote": clientIP(r),
			})
		}
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// metricsAllowed：默认只允许回环访问（Prometheus 与平台同机）；跨机抓取需设置
// ARTEX_METRICS_TOKEN 并带 X-Metrics-Token 头。
func metricsAllowed(r *http.Request) bool {
	if want := strings.TrimSpace(os.Getenv("ARTEX_METRICS_TOKEN")); want != "" {
		return r.Header.Get("X-Metrics-Token") == want
	}
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

// metricsHandler 输出 Prometheus 文本格式。指标集合保持小而稳定，便于 dashboard。
func (s *Server) metricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !metricsAllowed(r) {
			writeErr(w, http.StatusForbidden, "metrics 仅允许回环访问；跨机抓取请设置 ARTEX_METRICS_TOKEN 并携带 X-Metrics-Token")
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		writeMetricHeader(&b, "artex_up", "gauge", "服务进程存活")
		fmt.Fprintf(&b, "artex_up 1\n")
		writeMetricHeader(&b, "artex_build_info", "gauge", "构建版本信息")
		fmt.Fprintf(&b, "artex_build_info{version=%q} 1\n", BuildVersion)
		if !s.startedAt.IsZero() {
			writeMetricHeader(&b, "artex_uptime_seconds", "gauge", "进程启动时长")
			fmt.Fprintf(&b, "artex_uptime_seconds %.0f\n", time.Since(s.startedAt).Seconds())
		}

		s.metrics.mu.Lock()
		inFlight := s.metrics.inFlight
		reqKeys := make([]string, 0, len(s.metrics.requests))
		for k := range s.metrics.requests {
			reqKeys = append(reqKeys, k)
		}
		sort.Strings(reqKeys)
		reqSnap := make(map[string]int64, len(reqKeys))
		for _, k := range reqKeys {
			reqSnap[k] = s.metrics.requests[k]
		}
		methods := make([]string, 0, len(s.metrics.durCount))
		for k := range s.metrics.durCount {
			methods = append(methods, k)
		}
		sort.Strings(methods)
		durSum := make(map[string]float64, len(methods))
		durCnt := make(map[string]int64, len(methods))
		for _, m := range methods {
			durSum[m] = s.metrics.durSum[m]
			durCnt[m] = s.metrics.durCount[m]
		}
		s.metrics.mu.Unlock()
		writeMetricHeader(&b, "artex_http_requests_total", "counter", "HTTP 请求数（method/code）")
		for _, k := range reqKeys {
			method, code, _ := strings.Cut(k, "|")
			fmt.Fprintf(&b, "artex_http_requests_total{method=%q,code=%q} %d\n", method, code, reqSnap[k])
		}
		writeMetricHeader(&b, "artex_http_request_duration_seconds", "summary", "HTTP 请求耗时（method）")
		for _, m := range methods {
			fmt.Fprintf(&b, "artex_http_request_duration_seconds_sum{method=%q} %g\n", m, durSum[m])
			fmt.Fprintf(&b, "artex_http_request_duration_seconds_count{method=%q} %d\n", m, durCnt[m])
		}
		writeMetricHeader(&b, "artex_http_in_flight_requests", "gauge", "当前处理中的 HTTP 请求数")
		fmt.Fprintf(&b, "artex_http_in_flight_requests %d\n", inFlight)

		if s.m != nil {
			writeMetricHeader(&b, "artex_tasks", "gauge", "任务状态计数")
			writeMetricHeader(&b, "artex_tasks_total", "gauge", "任务总数")
			counts := s.m.TaskStatusCounts()
			totalTasks := 0
			for status, n := range counts {
				fmt.Fprintf(&b, "artex_tasks{status=%q} %d\n", status, n)
				totalTasks += n
			}
			fmt.Fprintf(&b, "artex_tasks_total %d\n", totalTasks)
			if pg := s.m.PG(); pg != nil {
				stats := pg.Stats()
				writeMetricHeader(&b, "artex_db_connections", "gauge", "数据库连接池状态")
				fmt.Fprintf(&b, "artex_db_connections{state=%q} %d\n", "open", stats.OpenConnections)
				fmt.Fprintf(&b, "artex_db_connections{state=%q} %d\n", "in_use", stats.InUse)
				fmt.Fprintf(&b, "artex_db_connections{state=%q} %d\n", "idle", stats.Idle)
				fmt.Fprintf(&b, "artex_db_max_open_connections %d\n", stats.MaxOpenConnections)
			}
		}
		if s.m != nil && s.m.pg != nil {
			if recs, err := db.NewC2IngressStore(s.m.pg).ListActive(r.Context()); err == nil {
				total, healthy := 0, 0
				for _, rec := range recs {
					total++
					if rec.Healthy {
						healthy++
					}
				}
				writeMetricHeader(&b, "artex_c2_ingress_total", "gauge", "C2 入站中继总数")
				fmt.Fprintf(&b, "artex_c2_ingress_total %d\n", total)
				writeMetricHeader(&b, "artex_c2_ingress_healthy", "gauge", "C2 入站中继健康数")
				fmt.Fprintf(&b, "artex_c2_ingress_healthy %d\n", healthy)
				writeMetricHeader(&b, "artex_c2_ingress_unhealthy", "gauge", "C2 入站中继异常数")
				fmt.Fprintf(&b, "artex_c2_ingress_unhealthy %d\n", total-healthy)
			}
		}
		if req, fail := s.c2BeaconCounters(); req > 0 || fail > 0 {
			writeMetricHeader(&b, "artex_c2_beacon_requests_total", "counter", "HTTP beacon 请求总数")
			fmt.Fprintf(&b, "artex_c2_beacon_requests_total %d\n", req)
			writeMetricHeader(&b, "artex_c2_beacon_failures_total", "counter", "HTTP beacon 失败总数")
			fmt.Fprintf(&b, "artex_c2_beacon_failures_total %d\n", fail)
		}
		if s.engine != nil {
			writeMetricHeader(&b, "artex_llm_active_calls", "gauge", "进行中的 LLM 调用数")
			fmt.Fprintf(&b, "artex_llm_active_calls %d\n", s.engine.SumActiveLLMCalls())
		}
		if s.maintenance != nil {
			writeMetricHeader(&b, "artex_scheduler_job_enabled", "gauge", "维护调度任务启用状态")
			writeMetricHeader(&b, "artex_scheduler_job_running", "gauge", "维护调度任务运行状态")
			writeMetricHeader(&b, "artex_scheduler_job_interval_seconds", "gauge", "维护调度任务间隔")
			writeMetricHeader(&b, "artex_scheduler_job_last_run_timestamp_seconds", "gauge", "维护调度任务上次运行时间")
			writeMetricHeader(&b, "artex_scheduler_job_last_error", "gauge", "维护调度任务上次是否失败")
			for _, job := range s.maintenance.Status() {
				name, _ := job["name"].(string)
				fmt.Fprintf(&b, "artex_scheduler_job_enabled{job=%q} %d\n", name, boolInt(job["enabled"]))
				fmt.Fprintf(&b, "artex_scheduler_job_running{job=%q} %d\n", name, boolInt(job["running"]))
				if v, ok := job["interval_seconds"]; ok {
					fmt.Fprintf(&b, "artex_scheduler_job_interval_seconds{job=%q} %v\n", name, v)
				}
				if v, ok := job["last_run_unix"]; ok {
					fmt.Fprintf(&b, "artex_scheduler_job_last_run_timestamp_seconds{job=%q} %v\n", name, v)
				}
				failed := 0
				if e, _ := job["last_error"].(string); e != "" {
					failed = 1
				}
				fmt.Fprintf(&b, "artex_scheduler_job_last_error{job=%q} %d\n", name, failed)
			}
		}
		writeMetricHeader(&b, "artex_go_goroutines", "gauge", "Go goroutine 数")
		fmt.Fprintf(&b, "artex_go_goroutines %d\n", runtime.NumGoroutine())
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		writeMetricHeader(&b, "artex_go_mem_alloc_bytes", "gauge", "Go 堆内存分配")
		fmt.Fprintf(&b, "artex_go_mem_alloc_bytes %d\n", ms.Alloc)
		_, _ = w.Write([]byte(b.String()))
	})
}

func writeMetricHeader(b *strings.Builder, name, typ, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func boolInt(v any) int {
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 0
}

// observabilityStatus 是给前端/运维看的 JSON 汇总（/metrics 之外的排障视图）。
func (s *Server) observabilityStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"version":     BuildVersion,
		"started_at":  s.startedAt,
		"uptime_secs": int(time.Since(s.startedAt).Seconds()),
		"log_format":  obsLogFormat(),
	}
	if s.m != nil {
		out["tasks"] = s.m.TaskStatusCounts()
		if pg := s.m.PG(); pg != nil {
			stats := pg.Stats()
			out["db"] = map[string]any{
				"open": stats.OpenConnections, "in_use": stats.InUse, "idle": stats.Idle,
				"max_open": stats.MaxOpenConnections,
			}
		}
	}
	if s.engine != nil {
		out["llm_active_calls"] = s.engine.SumActiveLLMCalls()
	}
	if s.maintenance != nil {
		out["scheduler"] = s.maintenance.Status()
	}
	s.metrics.mu.Lock()
	out["http"] = map[string]any{"in_flight": s.metrics.inFlight, "requests": len(s.metrics.requests)}
	s.metrics.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// --- 结构化日志 ---

func obsLogFormat() string {
	if v := strings.TrimSpace(os.Getenv("ARTEX_LOG_FORMAT")); v != "" {
		return strings.ToLower(v)
	}
	return "text"
}

func obsJSONEnabled() bool { return obsLogFormat() == "json" }

// obsLine 生成一行结构化日志（level/ts/msg + 字段）。纯函数便于测试。
func obsLine(level, msg string, fields map[string]any) string {
	rec := map[string]any{"ts": time.Now().Format(time.RFC3339Nano), "level": level, "msg": msg}
	for k, v := range fields {
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return "{\"level\":\"error\",\"msg\":\"log_marshal_failed\"}"
	}
	return string(b)
}

// obsLog 在 JSON 模式写单行 JSON 到 stderr；文本模式回落到标准 log。
func (s *Server) obsLog(level, msg string, fields map[string]any) {
	if obsJSONEnabled() {
		_, _ = fmt.Fprintln(os.Stderr, obsLine(level, msg, fields))
		return
	}
	if len(fields) == 0 {
		log.Printf("[%s] %s", level, msg)
		return
	}
	log.Printf("[%s] %s %v", level, msg, fields)
}
