package server

// 任务级 MITM 与上游热切换（M2-03）。
//
// 每个任务懒建独立 traffic.Traffic 实例（独立目录 → 独立 CA，防多实例首建竞争），
// 上游选择顺序：该任务最新 alive socks 隧道 → 全局出口代理 → 直连。隧道状态变化经
// tunnel.Manager.OnStateChange 触发 Refresh 热切换上游，不重建实例。
//
// 只在任务确有隧道时才建实例：默认无隧道的任务继续用平台全局出口，避免给每个任务
// 额外拉起一个 MITM。建实例失败只记日志并回落全局代理，绝不让录制子系统拖垮 worker。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
	"github.com/Autumn-27/artex/tunnel"
)

// tunnelLister 是任务隧道台账的最小读接口（*db.TunnelStore 满足；测试可注入）。
type tunnelLister interface {
	ListByTask(ctx context.Context, taskID int64) ([]*db.TunnelRecord, error)
}

type taskProxyInst struct {
	tr       *traffic.Traffic
	upstream string // 当前上游原文（""/global/socks5://...），变化检测用
}

type taskProxyManager struct {
	dir         string
	store       tunnelLister
	globalProxy func() string
	portMin     int
	portMax     int

	mu      sync.Mutex
	proxies map[int64]*taskProxyInst
}

func newTaskProxyManager(dir string, store tunnelLister, globalProxy func() string, portMin, portMax int) *taskProxyManager {
	if portMin <= 0 || portMax <= portMin {
		portMin, portMax = 21100, 21199
	}
	return &taskProxyManager{
		dir: filepath.Join(dir, "traffic-tasks"), store: store, globalProxy: globalProxy,
		portMin: portMin, portMax: portMax, proxies: map[int64]*taskProxyInst{},
	}
}

// pickUpstream 选任务上游：id 最大的 alive socks 隧道；suo5 入口带鉴权，URL 内嵌
// 台账 deploy_params 里的 user:pass；无可用隧道回落全局出口（可为 "" = 直连）。
// portfwd 不是上游形态，跳过。纯函数，便于表驱动测试。
func pickUpstream(recs []*db.TunnelRecord, globalProxy string) string {
	bestID := int64(-1)
	socksPort := 0
	var user, pass string
	for _, r := range recs {
		if r.State != db.TunnelAlive || r.Kind != tunnel.KindSocks || r.ID <= bestID {
			continue
		}
		var p tunnel.Plan
		if json.Unmarshal(r.DeployParams, &p) != nil || p.SocksPort <= 0 {
			continue
		}
		bestID, socksPort = r.ID, p.SocksPort
		user, pass = "", ""
		if r.Adapter == tunnel.AdapterSuo5 {
			user, pass = p.AuthUser, p.AuthPass
		}
	}
	if socksPort > 0 {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort))
		if user != "" {
			return "socks5://" + user + ":" + pass + "@" + addr
		}
		return "socks5://" + addr
	}
	return globalProxy
}

func (m *taskProxyManager) resolveUpstream(ctx context.Context, taskID int64) (string, error) {
	recs, err := m.store.ListByTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	gp := ""
	if m.globalProxy != nil {
		gp = m.globalProxy()
	}
	return pickUpstream(recs, gp), nil
}

// HasTunnels 报告任务是否已有隧道记录（决定是否为它单独建 MITM 实例）。
func (m *taskProxyManager) HasTunnels(ctx context.Context, taskID int64) bool {
	if m == nil || m.store == nil || taskID <= 0 {
		return false
	}
	recs, err := m.store.ListByTask(ctx, taskID)
	if err != nil {
		return false
	}
	return len(recs) > 0
}

// ForTask 返回任务 worker 应使用的代理地址与 CA：首次调用懒建实例（独立目录/CA +
// 端口池分配），之后每次调用顺手重算上游（事件间隙兜底）。失败返回 "","" → 调用方回落全局。
func (m *taskProxyManager) ForTask(ctx context.Context, taskID int64) (addr, caCert string) {
	if m == nil || taskID <= 0 {
		return "", ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.proxies[taskID]
	if inst == nil {
		created, err := m.buildLocked(ctx, taskID)
		if err != nil {
			log.Printf("[taskproxy] 任务 %d MITM 实例创建失败(回落全局代理): %v", taskID, err)
			return "", ""
		}
		inst = created
		m.proxies[taskID] = inst
		log.Printf("[taskproxy] 任务 %d MITM 实例就绪 %s", taskID, inst.tr.ProxyAddr())
	}
	m.refreshLocked(ctx, taskID, inst)
	return inst.tr.ProxyAddr(), inst.tr.CACertPath()
}

func (m *taskProxyManager) buildLocked(ctx context.Context, taskID int64) (*taskProxyInst, error) {
	if m.store == nil {
		return nil, fmt.Errorf("隧道台账未初始化")
	}
	up, err := m.resolveUpstream(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for port := m.portMin; port <= m.portMax; port++ {
		dir := filepath.Join(m.dir, strconv.FormatInt(taskID, 10))
		tr, err := traffic.Open(dir, "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			lastErr = err
			continue
		}
		if err := tr.SetUpstreamProxy(up); err != nil {
			tr.Close()
			return nil, fmt.Errorf("设置上游失败: %w", err)
		}
		return &taskProxyInst{tr: tr, upstream: up}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("端口池 %d-%d 无可用端口", m.portMin, m.portMax)
	}
	return nil, lastErr
}

func (m *taskProxyManager) refreshLocked(ctx context.Context, taskID int64, inst *taskProxyInst) {
	up, err := m.resolveUpstream(ctx, taskID)
	if err != nil {
		log.Printf("[taskproxy] 任务 %d 上游解析失败,保持现状: %v", taskID, err)
		return
	}
	if up == inst.upstream {
		return
	}
	if err := inst.tr.SetUpstreamProxy(up); err != nil {
		log.Printf("[taskproxy] 任务 %d 上游热切换失败(%s → %s): %v", taskID, inst.upstream, up, err)
		return
	}
	log.Printf("[taskproxy] 任务 %d 上游热切换: %q → %q", taskID, inst.upstream, up)
	inst.upstream = up
}

// Refresh 在隧道状态变化后重算任务上游（OnStateChange 回调入口）。
func (m *taskProxyManager) Refresh(ctx context.Context, taskID int64) {
	if m == nil || taskID <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst := m.proxies[taskID]; inst != nil {
		m.refreshLocked(ctx, taskID, inst)
	}
}

// CloseTask 关闭任务实例（任务删除/归档时调用）。
func (m *taskProxyManager) CloseTask(taskID int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst := m.proxies[taskID]; inst != nil {
		inst.tr.Close()
		delete(m.proxies, taskID)
	}
}

// CloseAll 关闭所有实例（进程退出）。
func (m *taskProxyManager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, inst := range m.proxies {
		inst.tr.Close()
		delete(m.proxies, id)
	}
}
