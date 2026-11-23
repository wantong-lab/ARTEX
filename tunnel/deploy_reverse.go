package tunnel

// deploy_reverse.go 反向入站中继：VPS 跑 chisel server（--reverse），平台跑
// chisel client 主动出站连 VPS，把 VPS 回调端口转发到平台 C2 loopback。
// 适用于平台在 NAT/手机热点、VPS 无法主动访问平台的场景。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

func (m *Manager) deployReverse(ctx context.Context, p *Plan, sess session.Session) (*db.TunnelRecord, error) {
	if err := m.prepareTarget(ctx, sess, p); err != nil {
		return nil, err
	}
	if err := m.deliverReverseBinary(ctx, sess, p); err != nil {
		return nil, err
	}
	rec := &db.TunnelRecord{
		TaskID: p.TaskID, Kind: p.Kind, Adapter: p.Adapter,
		ListenHost: p.ListenHost, ListenPort: p.ListenPort,
		TargetHost: p.TargetHost, TargetPort: p.TargetPort,
		ViaSessionID: p.ViaSessionID, State: db.TunnelDeploying,
	}
	raw, _ := json.Marshal(p)
	rec.DeployParams = raw
	id, err := m.store.Create(ctx, rec)
	if err != nil {
		return nil, fmt.Errorf("台账落库失败: %w", err)
	}
	if err := m.deployReverseInto(ctx, id, sess, p); err != nil {
		m.rollback(ctx, id, sess, p)
		return nil, err
	}
	if err := m.store.UpdateState(ctx, id, db.TunnelAlive, ""); err != nil {
		log.Printf("[tunnel] 状态回写失败(id=%d): %v", id, err)
	}
	m.notifyState(p.TaskID)
	rec, _ = m.store.Get(ctx, id)
	return rec, nil
}

func (m *Manager) deliverReverseBinary(ctx context.Context, sess session.Session, p *Plan) error {
	if p.ClientBinary == "" {
		return fmt.Errorf("反向 ingress 缺少 VPS 端 chisel 二进制")
	}
	data, err := os.ReadFile(p.ClientBinary)
	if err != nil {
		return fmt.Errorf("读取 VPS 端 chisel 失败: %w", err)
	}
	if _, _, err := sess.Exec(ctx, "mkdir -p "+p.RemoteDir, probeTimeout); err != nil {
		return fmt.Errorf("VPS 目录创建失败: %w", err)
	}
	if err := sess.WriteFile(ctx, p.RemoteBin, data); err != nil {
		return fmt.Errorf("VPS chisel 写入失败: %w", err)
	}
	return m.chmodRemote(ctx, sess, p)
}

func (m *Manager) deployReverseInto(ctx context.Context, id int64, sess session.Session, p *Plan) error {
	if err := sess.WriteFile(ctx, p.RemoteAuthFile, []byte(p.AuthfileContent())); err != nil {
		return fmt.Errorf("VPS authfile 写入失败: %w", err)
	}
	pid, err := m.spawnReverseServer(ctx, sess, p)
	if err != nil {
		return fmt.Errorf("VPS chisel server 启动失败: %w", err)
	}
	p.RemotePID = pid
	m.saveParams(ctx, id, p)
	time.Sleep(serverUpWait)
	if out, _, err := sess.Exec(ctx, "kill -0 "+strconv.Itoa(pid)+" 2>/dev/null && echo ARTEX_SRV_OK || echo ARTEX_SRV_DEAD", probeTimeout); err != nil || !strings.Contains(out, "ARTEX_SRV_OK") {
		return fmt.Errorf("VPS chisel server 秒退（日志 %s）", p.RemoteLog)
	}

	logFile, err := os.OpenFile(p.ServerLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("平台 chisel client 日志打开失败: %w", err)
	}
	cmd := exec.Command(m.chiselPath, p.ClientArgs...)
	cmd.Env = append(os.Environ(), "AUTH="+p.AuthUser+":"+p.AuthPass)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("平台 chisel client 启动失败: %w", err)
	}
	p.ServerPID = cmd.Process.Pid
	m.procs[id] = cmd
	m.saveParams(ctx, id, p)
	go func() { _ = cmd.Wait(); _ = logFile.Close() }()
	time.Sleep(serverUpWait)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("平台 chisel client 秒退（日志 %s）", p.ServerLog)
	}
	if err := m.verifyIngress(ctx, sess, p); err != nil {
		return fmt.Errorf("层 verify: %w", err)
	}
	return nil
}

func (m *Manager) spawnReverseServer(ctx context.Context, sess session.Session, p *Plan) (int, error) {
	pidFile := p.RemoteDir + "/server.pid"
	spawn := fmt.Sprintf("cd %s && { setsid nohup ./ch %s > %s 2>&1 < /dev/null & echo $! > %s; } >/dev/null 2>&1",
		p.RemoteDir, strings.Join(p.ServerArgs, " "), p.RemoteLog, pidFile)
	if _, _, err := sess.Exec(ctx, spawn, probeTimeout); err != nil {
		return 0, fmt.Errorf("拉起命令失败: %w", err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		stdout, _, err := sess.Exec(ctx, "cat "+pidFile+" 2>/dev/null", probeTimeout)
		if err == nil {
			fields := strings.Fields(strings.TrimSpace(stdout))
			if len(fields) > 0 {
				if pid, perr := strconv.Atoi(fields[0]); perr == nil && pid > 0 {
					return pid, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("未取到 VPS server pid")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
