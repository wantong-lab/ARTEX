package c2

// 管理器：TCP 监听 → token 握手 → 会话注册 → 任务派发/结果回收。
// 只为授权范围内的植入端服务；token 与信封密钥均在监听器创建时生成。

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// SessionInfo 是会话的只读快照（对外展示/落库用）。
type SessionInfo struct {
	ID       string
	Listener int64
	Remote   string
	OS       string
	Arch     string
	User     string
	LastSeen time.Time
}

type session struct {
	info    SessionInfo
	env     *Envelope
	conn    net.Conn
	mu      sync.Mutex
	pending map[uint32]chan *Result
	nextID  uint32
}

type listener struct {
	id     int64
	addr   string
	ln     net.Listener
	cancel context.CancelFunc
}

// Manager 是一个进程内的 C2 运行时（可挂多个监听器、多会话）。
type Manager struct {
	mu        sync.Mutex
	listeners map[int64]*listener
	sessions  map[string]*session
	token     string
	events    func(kind string, info SessionInfo)
	nextID    int64
}

func NewManager(token string) *Manager {
	return &Manager{listeners: map[int64]*listener{}, sessions: map[string]*session{}, token: token}
}

// SetEventHandler 注册会话事件回调（session_new/session_dead）。
func (m *Manager) SetEventHandler(f func(kind string, info SessionInfo)) { m.events = f }

// Token 返回监听器接入 token（植入端握手用）。
func (m *Manager) Token() string { return m.token }

// Start 在 addr 起一个 TCP 监听器，返回 listener id 与实际绑定地址（支持 :0）。
func (m *Manager) Start(ctx context.Context, addr string) (int64, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, "", fmt.Errorf("c2 监听失败: %w", err)
	}
	m.mu.Lock()
	m.nextID++
	id := m.nextID
	lctx, cancel := context.WithCancel(ctx)
	l := &listener{id: id, addr: ln.Addr().String(), ln: ln, cancel: cancel}
	m.listeners[id] = l
	m.mu.Unlock()
	go m.acceptLoop(lctx, l)
	return id, ln.Addr().String(), nil
}

// Stop 停止监听器并断开其下所有会话。
func (m *Manager) Stop(id int64) {
	m.mu.Lock()
	l := m.listeners[id]
	if l != nil {
		delete(m.listeners, id)
	}
	var conns []net.Conn
	for sid, s := range m.sessions {
		if s.info.Listener == id {
			conns = append(conns, s.conn)
			delete(m.sessions, sid)
		}
	}
	m.mu.Unlock()
	if l != nil {
		l.cancel()
		_ = l.ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.listeners))
	for id := range m.listeners {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// Adder 返回监听器实际绑定地址。
func (m *Manager) Addr(id int64) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.listeners[id]
	if !ok {
		return "", false
	}
	return l.addr, true
}

// Sessions 返回所有活跃会话快照。
func (m *Manager) Sessions() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.info)
	}
	return out
}

// Get 返回单个会话信息。
func (m *Manager) Get(id string) (SessionInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return SessionInfo{}, false
	}
	return s.info, true
}

func (m *Manager) acceptLoop(ctx context.Context, l *listener) {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			s, err := m.handshake(conn, l.id)
			if err != nil {
				_ = conn.Close()
				return
			}
			m.mu.Lock()
			m.sessions[s.info.ID] = s
			m.mu.Unlock()
			if m.events != nil {
				m.events("session_new", s.info)
			}
			m.readLoop(s)
		}()
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (m *Manager) handshake(conn net.Conn, listenerID int64) (*session, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	f, err := ReadFrame(conn)
	if err != nil || f.Type != MsgHello {
		return nil, fmt.Errorf("c2 握手失败: 非 hello 帧")
	}
	var h HelloPayload
	if err := decode(f.Payload, &h); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(h.Token), []byte(m.token)) != 1 {
		return nil, errors.New("c2 握手 token 校验失败")
	}
	env, err := ParseKey(h.Key)
	if err != nil {
		return nil, err
	}
	id := h.ID
	if id == "" {
		id = conn.RemoteAddr().String()
	}
	_ = conn.SetDeadline(time.Time{})
	info := SessionInfo{
		ID: id, Listener: listenerID, Remote: conn.RemoteAddr().String(),
		OS: h.OS, Arch: h.Arch, User: h.User, LastSeen: time.Now(),
	}
	s := &session{info: info, env: env, conn: conn, pending: map[uint32]chan *Result{}}
	// 回 welcome（明文，便于植入端确认平台身份）。
	ack, _ := encode(map[string]any{"ok": true})
	if err := WriteFrame(conn, &Frame{Type: MsgWelcome, Payload: ack}); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *Manager) readLoop(s *session) {
	defer func() {
		m.mu.Lock()
		delete(m.sessions, s.info.ID)
		m.mu.Unlock()
		if m.events != nil {
			m.events("session_dead", s.info)
		}
		_ = s.conn.Close()
	}()
	for {
		f, err := ReadFrame(s.conn)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.info.LastSeen = time.Now()
		s.mu.Unlock()
		switch f.Type {
		case MsgHeartbeat:
			continue
		case MsgBye:
			return
		case MsgResult:
			plain, err := s.env.Open(f.Payload)
			if err != nil {
				continue
			}
			var res Result
			if decode(plain, &res) != nil {
				continue
			}
			s.mu.Lock()
			ch := s.pending[f.ID]
			delete(s.pending, f.ID)
			s.mu.Unlock()
			if ch != nil {
				ch <- &res
			}
		}
	}
}

// Dispatch 向指定会话下发任务并等待结果（超时返回错误）。
func (m *Manager) Dispatch(ctx context.Context, sessionID string, t *Task, timeout time.Duration) (*Result, error) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("c2 会话不存在: %s", sessionID)
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	s.mu.Lock()
	s.nextID++
	taskID := s.nextID
	t.ID = taskID
	ch := make(chan *Result, 1)
	s.pending[taskID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, taskID)
		s.mu.Unlock()
	}()
	plain, err := encode(t)
	if err != nil {
		return nil, err
	}
	blob, err := s.env.Seal(plain)
	if err != nil {
		return nil, err
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	if err := WriteFrame(s.conn, &Frame{Type: MsgTask, ID: taskID, Payload: blob}); err != nil {
		return nil, fmt.Errorf("c2 下发失败: %w", err)
	}
	_ = s.conn.SetWriteDeadline(time.Time{})
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("c2 任务超时（%s）", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ClampText 截断任务输出，保护上层上下文。
func ClampText(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "\n…[输出截断]"
}

// IsLoopbackAddr 判断监听地址是否为回环（默认只允许回环绑定）。
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

var _ = atomic.Int64{}
