package memory

// 语义记忆层（移植自 Pentagi 的 pgvector 记忆设计,内核级集成）。
// findings/kb/pocs → embedding(bge-m3 via ollama) → pgvector →
// planner/worker 上下文注入「历史类似案例」。
// embedder 不可达时整体静默降级,不影响主流程。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/artex/db"
)

const (
	defaultEmbedURL   = "http://127.0.0.1:11434"
	defaultEmbedModel = "bge-m3"
	recallTopK        = 5
)

// Entry 一条语义记忆。
type Entry struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // finding|kb|poc|technique|fact
	RefID     string    `json:"ref_id"`
	TaskID    int64     `json:"task_id"`
	Content   string    `json:"content"`
	Score     float64   `json:"score"` // cosine distance(小=近)
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	d        *db.DB
	embedURL string
	model    string
	backend  string // ollama|openai
	mu       sync.Once
	enabled  bool
	degraded bool
	vecOK    bool
	dim      int
	cli      *http.Client
	queue    chan pendingItem // 实时路径(Add):非阻塞,队列满丢新——保护主流程
	queueLo  chan pendingItem // 批量回填路径(AddBlock):阻塞自节流,不丢
	closed   chan struct{}    // Close 关闭信号:worker 必须能退出
	wg       sync.WaitGroup
}

type pendingItem struct {
	kind, refID string
	taskID      int64
	content     string
}

func New(d *db.DB) *Store {
	u := envOr("ARTEX_EMBED_URL", defaultEmbedURL)
	m := envOr("ARTEX_EMBED_MODEL", defaultEmbedModel)
	dim := 1024
	if v := os.Getenv("ARTEX_EMBED_DIM"); v != "" {
		fmt.Sscanf(v, "%d", &dim)
	}
	s := &Store{
		d: d, embedURL: u, model: m, backend: envOr("ARTEX_EMBED_BACKEND", "ollama"),
		dim:     dim,
		cli:     &http.Client{Timeout: 15 * time.Second},
		queue:   make(chan pendingItem, 512),
		queueLo: make(chan pendingItem, 1024),
		closed:  make(chan struct{}),
	}
	// 离线降级语义:enabled 只看 DB;embedder 不可达置 degraded——
	// Add 照常入库(embedding 存 NULL),Recall 退化关键词检索,恢复后自动回填向量。
	s.enabled = s.d != nil
	if s.enabled {
		s.ensure()
		s.degraded = !s.probe()
		for i := 0; i < 2; i++ {
			s.wg.Add(1)
			go s.worker()
		}
	}
	return s
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func (s *Store) Enabled() bool { return s != nil && s.enabled }

// probe 测 embedder 是否可达（启动时一次）。
func (s *Store) probe() bool {
	if s.d == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ep := s.embedURL + "/api/tags"
	if s.backend == "openai" {
		ep = strings.TrimSuffix(s.embedURL, "/") + "/v1/models"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
	if k := os.Getenv("ARTEX_EMBED_KEY"); k != "" && s.backend == "openai" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := s.cli.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func (s *Store) ensure() {
	_, _ = s.d.Exec(`CREATE EXTENSION IF NOT EXISTS vector`)
	var has int
	_ = s.d.QueryRow(`SELECT 1 FROM pg_extension WHERE extname='vector'`).Scan(&has)
	s.vecOK = has == 1
	if !s.vecOK {
		// 无 pgvector：退化为关键词记忆（embedding 存文本，不参与向量检索）。
		_, _ = s.d.Exec(`CREATE TABLE IF NOT EXISTS mem_entries(
			id BIGSERIAL PRIMARY KEY, kind TEXT NOT NULL, ref_id TEXT NOT NULL DEFAULT '',
			task_id BIGINT NOT NULL DEFAULT 0, content TEXT NOT NULL,
			embedding TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(kind, ref_id))`)
		_, _ = s.d.Exec(`CREATE INDEX IF NOT EXISTS idx_mem_entries_kind ON mem_entries(kind)`)
		_, _ = s.d.Exec(`CREATE INDEX IF NOT EXISTS idx_mem_entries_task ON mem_entries(task_id)`)
		return
	}
	var oldDim int
	_ = s.d.QueryRow(`SELECT coalesce(atttypmod,0) FROM pg_attribute
		WHERE attrelid='mem_entries'::regclass AND attname='embedding'`).Scan(&oldDim)
	if oldDim > 0 && oldDim != s.dim {
		_, _ = s.d.Exec(`DROP TABLE mem_entries`) // 维度不匹配→重建(重灌由 reindex 完成)
	}
	_, _ = s.d.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS mem_entries(
		id BIGSERIAL PRIMARY KEY, kind TEXT NOT NULL, ref_id TEXT NOT NULL DEFAULT '',
		task_id BIGINT NOT NULL DEFAULT 0, content TEXT NOT NULL,
		embedding vector(%d), created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(kind, ref_id))`, s.dim))
	_, _ = s.d.Exec(`CREATE INDEX IF NOT EXISTS idx_mem_entries_kind ON mem_entries(kind)`)
	_, _ = s.d.Exec(`CREATE INDEX IF NOT EXISTS idx_mem_entries_task ON mem_entries(task_id)`)
	_, _ = s.d.Exec(`CREATE INDEX IF NOT EXISTS idx_mem_entries_vec ON mem_entries
		USING hnsw (embedding vector_cosine_ops)`)
}

// embed 取文本向量。后端两种:
//   - ollama(默认): POST {url}/api/embed {model,input} → embeddings[0]
//   - openai: POST {url}/v1/embeddings {model,input} → data[0].embedding
//     (OpenAI 兼容端点,ARTEX_EMBED_KEY 可带 Bearer)
func (s *Store) embed(ctx context.Context, text string) ([]float32, error) {
	// llama-server ubatch 上限 2048 token;中文 1 字≈1-2 token,截到 ~2900 字节
	// 保证任何输入都低于批次上限,避免超限条目每轮 tick 空转重试。
	const maxEmbedBytes = 2900
	if len(text) > maxEmbedBytes {
		text = text[:maxEmbedBytes]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	if s.backend == "openai" {
		return s.embedOpenAI(ctx, text)
	}
	return s.embedOllama(ctx, text)
}

func (s *Store) embedOllama(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": s.model, "input": text,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", s.embedURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 || len(out.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return out.Embeddings[0], nil
}

func (s *Store) embedOpenAI(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": s.model, "input": text,
	})
	url := strings.TrimSuffix(s.embedURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("ARTEX_EMBED_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := s.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return out.Data[0].Embedding, nil
}

// Add 异步索引一条记忆（embed + upsert,不阻塞调用方）。
func (s *Store) Add(kind, refID string, taskID int64, content string) {
	if !s.Enabled() || strings.TrimSpace(content) == "" {
		return
	}
	select {
	case s.queue <- pendingItem{kind: kind, refID: refID, taskID: taskID, content: content}:
	default: // 队列满丢最旧,防反压卡主流程
	}
}

// AddBlock 批量回填路径:队列满时阻塞等待而不是丢弃——memdaemon 水位线
// 语义因此成立(入队成功才推进水位)。仅供后台索引器用,实时路径仍走 Add。
func (s *Store) AddBlock(kind, refID string, taskID int64, content string) {
	if !s.Enabled() || strings.TrimSpace(content) == "" {
		return
	}
	s.queueLo <- pendingItem{kind: kind, refID: refID, taskID: taskID, content: content}
}

// AddSync 同步索引（reindex 批量用）。
func (s *Store) AddSync(ctx context.Context, kind, refID string, taskID int64, content string) error {
	if !s.Enabled() {
		return fmt.Errorf("memory disabled")
	}
	vec, err := s.embed(ctx, content)
	if err != nil {
		s.degraded = true
		return s.upsert(kind, refID, taskID, content, nil)
	}
	s.degraded = false
	return s.upsert(kind, refID, taskID, content, vec)
}

func (s *Store) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		var it pendingItem
		// 高优先级队列优先干:实时写入(kb_add/finding/technique)永远排在
		// 批量回填前面,不被存量追平饿死。
		select {
		case it = <-s.queue:
		default:
			select {
			case it = <-s.queue:
			case it = <-s.queueLo:
			case <-s.closed:
				return
			}
		}
		s.embedUpsert(it)
	}
}

func (s *Store) embedUpsert(it pendingItem) {
	if strings.TrimSpace(it.content) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	vec, err := s.embed(ctx, it.content)
	cancel()
	if err != nil {
		s.degraded = true
		_ = s.upsert(it.kind, it.refID, it.taskID, it.content, nil)
		return
	}
	s.degraded = false
	_ = s.upsert(it.kind, it.refID, it.taskID, it.content, vec)
	s.backfillNulls(8)
}

// backfillNulls embedder 恢复后把存量 NULL 向量行补成真向量,自愈降级期数据。
func (s *Store) backfillNulls(n int) {
	if !s.vecOK {
		return
	}
	rows, err := s.d.Query(`SELECT id,content FROM mem_entries WHERE embedding IS NULL ORDER BY id LIMIT ` + fmt.Sprint(n))
	if err != nil {
		return
	}
	defer rows.Close()
	var items []struct {
		id int64
		c  string
	}
	for rows.Next() {
		var it struct {
			id int64
			c  string
		}
		if rows.Scan(&it.id, &it.c) == nil {
			items = append(items, it)
		}
	}
	for _, it := range items {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		vec, err := s.embed(ctx, it.c)
		cancel()
		if err != nil {
			s.degraded = true
			return
		}
		_, _ = s.d.Exec(`UPDATE mem_entries SET embedding=$2::vector WHERE id=$1`, it.id, vecLit(vec))
	}
}

func vecLit(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}

func (s *Store) upsert(kind, refID string, taskID int64, content string, vec []float32) error {
	var vecSQL any
	if vec != nil {
		vecSQL = vecLit(vec)
	}
	if strings.IndexByte(content, 0) >= 0 {
		content = strings.ReplaceAll(content, "\x00", "")
	}
	content = strings.ToValidUTF8(content, "�")
	if s.vecOK {
		_, err := s.d.Exec(`INSERT INTO mem_entries(kind,ref_id,task_id,content,embedding)
			VALUES($1,$2,$3,$4,$5::vector)
			ON CONFLICT(kind,ref_id) DO UPDATE SET content=EXCLUDED.content,
			embedding=COALESCE(EXCLUDED.embedding, mem_entries.embedding),task_id=EXCLUDED.task_id`,
			kind, refID, taskID, content, vecSQL)
		return err
	}
	_, err := s.d.Exec(`INSERT INTO mem_entries(kind,ref_id,task_id,content,embedding)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(kind,ref_id) DO UPDATE SET content=EXCLUDED.content,
		task_id=EXCLUDED.task_id`,
		kind, refID, taskID, content, vecSQL)
	return err
}

// Recall 语义召回 top-K（cosine 距离升序,小=近）。kind 空=全类别。
func (s *Store) Recall(ctx context.Context, query string, kind string, k int) ([]*Entry, error) {
	var kinds []string
	if kind != "" {
		kinds = []string{kind}
	}
	return s.recallKinds(ctx, query, kinds, k)
}

// recallKinds 限定 kind 集合的语义召回(kinds 空=全类别)。
func (s *Store) recallKinds(ctx context.Context, query string, kinds []string, k int) ([]*Entry, error) {
	if !s.Enabled() {
		return nil, nil
	}
	if k <= 0 {
		k = recallTopK
	}
	if !s.vecOK {
		return s.recallKeyword(ctx, query, kinds, k)
	}
	vec, err := s.embed(ctx, query)
	if err != nil {
		s.degraded = true
		return s.recallKeyword(ctx, query, kinds, k)
	}
	s.degraded = false
	q := `SELECT id,kind,ref_id,task_id,content,(embedding <=> $1::vector) AS dist,created_at
		FROM mem_entries`
	args := []any{vecLit(vec)}
	if len(kinds) > 0 {
		q += ` WHERE kind=ANY($2)`
		args = append(args, kinds)
	}
	q += ` ORDER BY dist ASC LIMIT ` + fmt.Sprint(k)
	rows, err := s.d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e := &Entry{}
		if err := rows.Scan(&e.ID, &e.Kind, &e.RefID, &e.TaskID, &e.Content, &e.Score, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// 注入分层:第一梯队=蒸馏经验(打法/避坑/弹药笔记/发现),直接进 top-K;
// 第二梯队=原料级(fact 现场碎片/poc 脚本/命令 dump),要求更高相关度;
// fact 只允许同任务(存量 task_id=0 的全拦——别任务的端口矩阵/会话
// 碎片对当前任务是噪音;平台级情报应走 kb_add 沉淀而非 fact)。
var (
	recallPrimary   = []string{"technique", "negative", "kb", "finding"}
	recallSecondary = []string{"fact", "poc", "successful_tool"}
	// 次梯队最大余弦距离(小=近):约等价于相关度≥0.55。
	secondaryMaxDist = 0.45
	// 注入条数上限:第一梯队 4 + 次梯队补齐到 6。
	recallBlockCap = 6
)

// RecallBlock 渲染给 worker/planner 上下文注入用的文本块。
func (s *Store) RecallBlock(ctx context.Context, query string, taskID int64) string {
	ents, err := s.recallKinds(ctx, query, recallPrimary, 4)
	if err != nil {
		return ""
	}
	if sec, err2 := s.recallKinds(ctx, query, recallSecondary, 4); err2 == nil {
		for _, e := range sec {
			if len(ents) >= recallBlockCap {
				break
			}
			if e.Score > secondaryMaxDist {
				continue
			}
			if e.Kind == "fact" && e.TaskID != taskID {
				continue
			}
			ents = append(ents, e)
		}
	}
	if len(ents) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【历史相似经验(语义召回,按相关度排序,参考勿照抄)】：\n")
	for i, e := range ents {
		c := e.Content
		if len(c) > 400 {
			c = c[:400] + "…"
		}
		fmt.Fprintf(&b, "%d. [%s] (相关度 %.2f) %s\n", i+1, e.Kind, 1.0-e.Score, c)
	}
	return b.String()
}

// ExistingRefIDs 返回某 kind 已索引的 ref_id 集合(增量索引去重/跳过重嵌入用)。
func (s *Store) ExistingRefIDs(kind string) map[string]bool {
	out := map[string]bool{}
	if !s.Enabled() {
		return out
	}
	rows, err := s.d.Query(`SELECT ref_id FROM mem_entries WHERE kind=$1`, kind)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil {
			out[r] = true
		}
	}
	return out
}

// Count 索引条目数(健康检查用)。
func (s *Store) Count() int {
	if !s.Enabled() {
		return 0
	}
	var n int
	_ = s.d.QueryRow(`SELECT count(*) FROM mem_entries`).Scan(&n)
	return n
}

// Close 通知 worker 退出并等待排空。用 closed 通道而不是 close(queue)：
// close(queue) 会让 worker 立刻收到零值并空转（fork 的原始实现有这个缺陷）。
func (s *Store) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.closed:
		return // 幂等
	default:
		close(s.closed)
	}
	s.wg.Wait()
}

// recallKeyword embedder 离线时的降级召回：从 query 抽关键词做 ILIKE OR 匹配,
// 按时间倒序取 top-K。Score 固定 0.9,在注入块里呈现为低相关度,如实反映弱匹配。
func (s *Store) recallKeyword(ctx context.Context, query string, kinds []string, k int) ([]*Entry, error) {
	terms := keywordTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	var conds []string
	args := []any{}
	for _, t := range terms {
		conds = append(conds, "content ILIKE $"+fmt.Sprint(len(args)+1))
		args = append(args, "%"+t+"%")
	}
	q := `SELECT id,kind,ref_id,task_id,content,0.9 AS dist,created_at FROM mem_entries WHERE (` + strings.Join(conds, " OR ") + `)`
	if len(kinds) > 0 {
		q += ` AND kind=ANY($` + fmt.Sprint(len(args)+1) + `)`
		args = append(args, kinds)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + fmt.Sprint(k)
	rows, err := s.d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e := &Entry{}
		if err := rows.Scan(&e.ID, &e.Kind, &e.RefID, &e.TaskID, &e.Content, &e.Score, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// keywordTerms 从查询里抽 ILIKE 关键词：2 字以上的中文片段和字母数字串,去重限 6 个。
var kwRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9._-]{1,}|[一-龥]{2,}`)

var kwStop = map[string]bool{"http": true, "https": true, "www": true, "com": true, "cn": true,
	"目标": true, "发现": true, "存在": true, "进行": true, "利用": true, "漏洞": true,
	"端口": true, "服务": true, "系统": true, "请求": true, "响应": true, "测试": true, "获取": true}

func keywordTerms(q string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range kwRe.FindAllString(q, -1) {
		l := strings.ToLower(t)
		if len(t) < 2 || len(t) > 32 || kwStop[l] || kwStop[t] || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// Degraded 报告 embedder 当前处于降级态(召回走关键词,新增条目无向量待回填)。
func (s *Store) Degraded() bool { return s != nil && s.enabled && s.degraded }
