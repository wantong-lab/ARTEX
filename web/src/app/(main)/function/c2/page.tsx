"use client";

import { useCallback, useEffect, useState } from "react";

import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { C2Ingress, C2IngressEvent, C2Listener, C2Session, C2Task, IntranetSession } from "@/lib/types";

function fmtTime(v?: string): string {
  if (!v) return "—";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString("zh-CN", { hour12: false });
}

const STATUS_LABEL: Record<string, string> = {
  running: "运行中",
  stopped: "已停止",
  error: "错误",
  active: "在线",
  dead: "失联",
  killed: "已终止",
  alive: "在线",
  deploying: "部署中",
  queued: "排队",
  sent: "已下发",
  success: "成功",
  failed: "失败",
  cancelled: "取消",
};

export default function C2Page() {
  const [enabled, setEnabled] = useState(false);
  const [listeners, setListeners] = useState<C2Listener[]>([]);
  const [sessions, setSessions] = useState<C2Session[]>([]);
  const [tasks, setTasks] = useState<C2Task[]>([]);
  const [busy, setBusy] = useState("");
  const [loaded, setLoaded] = useState(false);

  const [name, setName] = useState("beacon-1");
  const [kind, setKind] = useState("http");
  const [bindHost, setBindHost] = useState("127.0.0.1");
  const [bindPort, setBindPort] = useState("19660");

  const [command, setCommand] = useState("id");
  const [sessionId, setSessionId] = useState("");
  const [implantListener, setImplantListener] = useState("");
  const [implantLang, setImplantLang] = useState("python");
  const [implantContent, setImplantContent] = useState("");

  const [ingress, setIngress] = useState<C2Ingress[]>([]);
  const [vpsSessions, setVpsSessions] = useState<IntranetSession[]>([]);
  const [ingHealthFilter, setIngHealthFilter] = useState("all");
  const [ingNodeFilter, setIngNodeFilter] = useState("");
  const [ingressEvents, setIngressEvents] = useState<C2IngressEvent[]>([]);
  const [ingSession, setIngSession] = useState("");
  const [ingVpsHost, setIngVpsHost] = useState("");
  const [ingVpsPort, setIngVpsPort] = useState("39444");
  const [ingKind, setIngKind] = useState("http");
  const [ingNodeLabel, setIngNodeLabel] = useState("");
  const [ingPlatformHost, setIngPlatformHost] = useState("");
  const [ingPlatformPort, setIngPlatformPort] = useState("39443");
  const [ingImplantLang, setIngImplantLang] = useState("powershell");
  const [ingImplantContent, setIngImplantContent] = useState("");

  const refresh = useCallback(async () => {
    const [st, ls, ss, ts, ing, ev, vs] = await Promise.all([
      api.c2Status(),
      api.c2Listeners(),
      api.c2Sessions(),
      api.c2Tasks(),
      api.c2Ingress(),
      api.c2IngressEvents(),
      api.intranetSessions(),
    ]);
    setEnabled(st.enabled);
    setListeners(ls.listeners ?? []);
    setSessions(ss.sessions ?? []);
    setTasks(ts.tasks ?? []);
    setIngress(ing.ingress ?? []);
    setIngressEvents(ev.events ?? []);
    setLoaded(true);
  }, []);

  useEffect(() => {
    refresh().catch((e: Error) => toast.error("加载 C2 状态失败：" + e.message));
  }, [refresh]);

  useEffect(() => {
    if (!enabled) return;
    const t = window.setInterval(() => {
      refresh().catch(() => {});
    }, 8000);
    return () => window.clearInterval(t);
  }, [enabled, refresh]);

  async function toggleEnabled(next: boolean) {
    if (next && !window.confirm("C2 会绑定监听端口并接收植入端回连，请确认已获得授权。启用？")) return;
    setBusy("enable");
    try {
      await api.c2Enable(next);
      toast.success(next ? "C2 已启用" : "C2 已停用");
      await refresh();
    } catch (e) {
      toast.error("操作失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function createListener() {
    if (!name.trim()) {
      toast.error("监听器名称必填");
      return;
    }
    const port = Number(bindPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast.error("端口必须在 1-65535");
      return;
    }
    setBusy("create");
    try {
      const r = await api.c2CreateListener({ name: name.trim(), kind, bind_host: bindHost.trim() || "127.0.0.1", bind_port: port });
      toast.success("监听器已创建（id=" + r.id + "，token 仅创建时生成并加密存储）");
      await refresh();
    } catch (e) {
      toast.error("创建失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function listenerAction(id: number, action: "start" | "stop" | "delete") {
    setBusy(action + ":" + id);
    try {
      if (action === "start") await api.c2StartListener(id);
      if (action === "stop") await api.c2StopListener(id);
      if (action === "delete") {
        if (!window.confirm("删除监听器会级联删除其会话与任务记录，确认？")) {
          setBusy("");
          return;
        }
        await api.c2DeleteListener(id);
      }
      toast.success("操作成功");
      await refresh();
    } catch (e) {
      toast.error("操作失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function queueTask() {
    const sid = Number(sessionId);
    if (!sid) {
      toast.error("请先选择会话");
      return;
    }
    if (!command.trim()) {
      toast.error("命令不能为空");
      return;
    }
    setBusy("queue");
    try {
      const r = await api.c2QueueTask(sid, command.trim());
      toast.success("任务已下发 #" + r.id + "（HTTP 植入端下次心跳领取）");
      await refresh();
    } catch (e) {
      toast.error("下发失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function generateImplant() {
    const lid = Number(implantListener);
    if (!lid) {
      toast.error("请先选择 HTTP 监听器");
      return;
    }
    setBusy("implant");
    try {
      const r = await api.c2Implant(lid, implantLang);
      setImplantContent(r.content);
      toast.success("植入端已生成（base_url=" + r.base_url + "）");
    } catch (e) {
      toast.error("生成失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }


  async function createIngress() {
    const sid = Number(ingSession);
    const port = Number(ingVpsPort);
    const pp = Number(ingPlatformPort);
    if (!sid) { toast.error("VPS 会话 id 必填"); return; }
    if (!ingVpsHost.trim()) { toast.error("VPS 公网 host 必填"); return; }
    if (!Number.isInteger(port) || port < 1 || port > 65535) { toast.error("VPS 回调端口非法"); return; }
    setBusy("ingress");
    try {
      const r = await api.c2CreateIngress({
        session_id: sid, vps_host: ingVpsHost.trim(), vps_port: port,
        c2_listener_kind: ingKind, node_label: ingNodeLabel.trim(),
        platform_host: ingPlatformHost.trim(), platform_chisel_port: pp || undefined,
        implant_lang: ingImplantLang,
      });
      setIngImplantContent(r.implant || "");
      toast.success("入站中继已部署 tunnel=" + r.tunnel_id + " callback=" + r.callback_addr);
      await refresh();
    } catch (e) {
      toast.error("创建失败：" + (e as Error).message);
    } finally { setBusy(""); }
  }

  async function teardownIngress(id: number) {
    if (!window.confirm("停止中继会停隧道并关闭关联 C2 listener，确认？")) return;
    setBusy("ingress:" + id);
    try {
      await api.c2TeardownIngress(id);
      toast.success("入站中继已停止");
      await refresh();
    } catch (e) {
      toast.error("停止失败：" + (e as Error).message);
    } finally { setBusy(""); }
  }


  async function rebuildIngress(id: number) {
    if (!window.confirm("重建会先停止当前中继再用原会话/节点重新部署，确认？")) return;
    setBusy("rebuild:" + id);
    try {
      const r = await api.c2RebuildIngress(id);
      setIngImplantContent(r.implant || "");
      toast.success("已重建 tunnel=" + r.tunnel_id + " callback=" + r.callback_addr);
      await refresh();
    } catch (e) {
      toast.error("重建失败：" + (e as Error).message);
    } finally { setBusy(""); }
  }

  const nodeOptions = Array.from(new Set(ingress.map((v) => v.node_label).filter(Boolean)));
  const healthyCount = ingress.filter((v) => v.healthy).length;
  const filteredIngress = ingress.filter((v) => {
    if (ingHealthFilter === "healthy" && !v.healthy) return false;
    if (ingHealthFilter === "unhealthy" && v.healthy) return false;
    if (ingNodeFilter && v.node_label !== ingNodeFilter) return false;
    return true;
  });
  const httpListeners = listeners.filter((l) => l.kind === "http");
  const e = (v: string) => v || "—";

  return (
    <div className="flex flex-col gap-4 px-4 lg:px-6">
      <header className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">C2 管理</h1>
          <p className="text-muted-foreground text-sm">
            默认关闭；启用后才会绑定监听端口。HTTP beacon 植入端通过 /c2/beacon/&lt;token&gt; 轮询任务。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant={enabled ? "default" : "secondary"}>{enabled ? "已启用" : "已关闭"}</Badge>
          <Button size="sm" disabled={!loaded || busy !== ""} onClick={() => void toggleEnabled(!enabled)}>
            {enabled ? "停用 C2" : "启用 C2"}
          </Button>
          <Button variant="outline" size="sm" disabled={busy !== ""} onClick={() => void refresh()}>
            刷新
          </Button>
        </div>
      </header>

      {!enabled ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive">
          C2 当前未启用：监听器不会启动，工具与植入端均不可用。确认授权后点击「启用 C2」。
        </div>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">先看这里：选哪种方式？</CardTitle>
          <CardDescription>先判断目标能不能直接访问平台；能直连走方式 A，只能访问 VPS 走方式 B。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 text-sm">
          <div className="grid gap-3 md:grid-cols-2">
            <div className="rounded-md border p-3">
              <div className="font-medium">方式 A：目标能直接访问平台</div>
              <p className="text-muted-foreground mt-1">创建平台监听器 → 生成植入端。适合内网目标或平台地址可达的情况。</p>
              <p className="text-muted-foreground mt-1">一般只需要：监听器（HTTP，127.0.0.1）→ 生成植入端。</p>
            </div>
            <div className="rounded-md border p-3">
              <div className="font-medium">方式 B：目标只能访问 VPS</div>
              <p className="text-muted-foreground mt-1">填写 VPS 会话/公网地址/回调端口 → 创建入站中继 → 复制生成的植入端。</p>
              <p className="text-muted-foreground mt-1">目标只看到 VPS，不暴露平台地址。</p>
            </div>
          </div>
          <details className="rounded-md border p-3">
            <summary className="cursor-pointer font-medium">字段速查（点开）</summary>
            <div className="mt-2 grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
              <div><span className="text-foreground">监听器</span>：平台侧接收植入端连接的入口，通常 HTTP + 127.0.0.1 即可。</div>
              <div><span className="text-foreground">入站中继</span>：目标回连 VPS，VPS 再转发到平台；适合目标只能访问 VPS。</div>
              <div><span className="text-foreground">VPS 会话</span>：平台已有的到 VPS 的会话 id（intranet sessions）。</div>
              <div><span className="text-foreground">VPS 公网地址/端口</span>：目标实际回连的地址和端口，端口需在 VPS 防火墙放行。</div>
              <div><span className="text-foreground">平台 chisel 地址/端口</span>：VPS 回连平台用的地址/端口，通常保持默认 39443。</div>
              <div><span className="text-foreground">植入端</span>：在目标上运行的脚本；方式 B 创建中继后会直接给出。</div>
            </div>
          </details>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">① 平台监听器（通常保持默认）</CardTitle>
            <CardDescription>平台接收植入端连接的入口。一般保持 HTTP + 127.0.0.1；只有需要目标直连平台时才考虑其他绑定。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="grid gap-2 sm:grid-cols-5">
              <div className="sm:col-span-2">
                <Label className="text-xs">名称</Label>
                <Input value={name} onChange={(ev) => setName(ev.target.value)} />
              </div>
              <div>
                <Label className="text-xs">类型</Label>
                <select
                  className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm"
                  value={kind}
                  onChange={(ev) => setKind(ev.target.value)}
                >
                  <option value="http">HTTP beacon</option>
                  <option value="tcp">TCP</option>
                </select>
              </div>
              <div>
                <Label className="text-xs">端口</Label>
                <Input value={bindPort} onChange={(ev) => setBindPort(ev.target.value)} />
              </div>
              <div className="flex items-end">
                <Button size="sm" disabled={busy !== ""} onClick={() => void createListener()}>
                  创建
                </Button>
              </div>
            </div>
            <Table
              head={["ID", "名称", "类型", "地址", "状态", "操作"]}
              rows={listeners.map((l) => [
                String(l.id),
                l.name || "—",
                l.kind,
                l.bind_host + ":" + l.bind_port,
                <Badge key={"st" + l.id} variant={l.status === "running" ? "default" : "secondary"}>
                  {STATUS_LABEL[l.status] || l.status}
                </Badge>,
                <span key={"op" + l.id} className="flex justify-end gap-1">
                  {l.status !== "running" ? (
                    <Button size="sm" variant="outline" disabled={busy !== ""} onClick={() => void listenerAction(l.id, "start")}>
                      启动
                    </Button>
                  ) : (
                    <Button size="sm" variant="outline" disabled={busy !== ""} onClick={() => void listenerAction(l.id, "stop")}>
                      停止
                    </Button>
                  )}
                  <Button size="sm" variant="destructive" disabled={busy !== ""} onClick={() => void listenerAction(l.id, "delete")}>
                    删除
                  </Button>
                </span>,
              ])}
              empty="暂无监听器"
            />
            <div className="grid gap-2 sm:grid-cols-2">
              <div>
                <Label className="text-xs">绑定主机</Label>
                <Input value={bindHost} onChange={(ev) => setBindHost(ev.target.value)} />
              </div>
              <div className="flex items-end text-xs text-muted-foreground">
                绑定 0.0.0.0 时，植入端 base_url 需在生成接口用 base_url 覆盖为可达地址。
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">③ 生成植入端</CardTitle>
            <CardDescription>方式 A 使用；选择监听器后生成脚本。方式 B 在入站中继创建后已直接生成，不需要在这里重复。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="grid gap-2 sm:grid-cols-3">
              <div className="sm:col-span-2">
                <Label className="text-xs">监听器</Label>
                <select
                  className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm"
                  value={implantListener}
                  onChange={(ev) => setImplantListener(ev.target.value)}
                >
                  <option value="">选择 HTTP 监听器…</option>
                  {httpListeners.map((l) => (
                    <option key={l.id} value={String(l.id)}>
                      #{l.id} {l.name} ({l.bind_host}:{l.bind_port})
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <Label className="text-xs">语言</Label>
                <select
                  className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm"
                  value={implantLang}
                  onChange={(ev) => setImplantLang(ev.target.value)}
                >
                  <option value="python">Python</option>
                  <option value="bash">Bash</option>
                  <option value="powershell">PowerShell</option>
                </select>
              </div>
            </div>
            <div className="flex gap-2">
              <Button size="sm" disabled={busy !== ""} onClick={() => void generateImplant()}>
                生成植入端
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!implantContent}
                onClick={() => {
                  navigator.clipboard.writeText(implantContent).then(
                    () => toast.success("已复制"),
                    () => toast.error("复制失败"),
                  );
                }}
              >
                复制
              </Button>
            </div>
            <textarea
              className="border-input bg-muted/30 h-56 w-full rounded-md border p-2 font-mono text-xs"
              readOnly
              placeholder="生成后在此显示植入端脚本"
              value={implantContent}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">② 入站中继（目标只能访问 VPS 时用）</CardTitle>
          <CardDescription>目标只回连 VPS；平台监听器只绑 127.0.0.1，VPS 通过 chisel 把流量转发回平台。页面每 30s 更新健康状态。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="grid gap-2 sm:grid-cols-4">
            <div>
              <Label className="text-xs">1. 到 VPS 的会话</Label>
              <select className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm" value={ingSession} onChange={(ev) => setIngSession(ev.target.value)}>
                <option value="">选择 VPS 会话…</option>
                {vpsSessions.map((vs) => (
                  <option key={vs.id} value={String(vs.id)}>
                    #{vs.id} {vs.kind} {vs.url} ({vs.status})
                  </option>
                ))}
              </select>
            </div>
            <div>
              <Label className="text-xs">2. VPS 公网 IP/域名</Label>
              <Input value={ingVpsHost} onChange={(ev) => setIngVpsHost(ev.target.value)} placeholder="1.2.3.4" />
            </div>
            <div>
              <Label className="text-xs">VPS 回调端口（目标回连）</Label>
              <Input value={ingVpsPort} onChange={(ev) => setIngVpsPort(ev.target.value)} />
            </div>
            <div>
              <Label className="text-xs">植入端类型</Label>
              <select className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm" value={ingKind} onChange={(ev) => setIngKind(ev.target.value)}>
                <option value="http">HTTP beacon</option>
                <option value="tcp">TCP</option>
              </select>
            </div>
            <div>
              <Label className="text-xs">节点标签（可选）</Label>
              <Input value={ingNodeLabel} onChange={(ev) => setIngNodeLabel(ev.target.value)} placeholder="vps-hk-1" />
            </div>
            <div>
              <Label className="text-xs">平台 chisel 地址</Label>
              <Input value={ingPlatformHost} onChange={(ev) => setIngPlatformHost(ev.target.value)} placeholder="平台公网 IP" />
            </div>
            <div>
              <Label className="text-xs">平台 chisel 端口</Label>
              <Input value={ingPlatformPort} onChange={(ev) => setIngPlatformPort(ev.target.value)} />
            </div>
            <div>
              <Label className="text-xs">脚本类型</Label>
              <select className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm" value={ingImplantLang} onChange={(ev) => setIngImplantLang(ev.target.value)}>
                <option value="powershell">PowerShell</option>
                <option value="python">Python</option>
                <option value="bash">Bash</option>
              </select>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">不确定怎么填：VPS 回调端口填 39444，平台 chisel 端口保持默认 39443；详细说明见 docs/c2-ingress-runbook.md。</p>
          <div className="flex gap-2">
            <Button size="sm" disabled={busy !== ""} onClick={() => void createIngress()}>创建中继</Button>
            <Button size="sm" variant="outline" disabled={!ingImplantContent} onClick={() => {
              navigator.clipboard.writeText(ingImplantContent).then(() => toast.success("已复制"), () => toast.error("复制失败"));
            }}>复制植入端</Button>
          </div>
          <textarea className="border-input bg-muted/30 h-32 w-full rounded-md border p-2 font-mono text-xs" readOnly placeholder="创建后显示植入端脚本" value={ingImplantContent} />
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="text-muted-foreground">共 {ingress.length} 个中继，健康 {healthyCount}</span>
            <select className="border-input bg-background h-8 rounded-md border px-2" value={ingHealthFilter} onChange={(ev) => setIngHealthFilter(ev.target.value)}>
              <option value="all">全部健康状态</option>
              <option value="healthy">仅健康</option>
              <option value="unhealthy">仅异常</option>
            </select>
            <select className="border-input bg-background h-8 rounded-md border px-2" value={ingNodeFilter} onChange={(ev) => setIngNodeFilter(ev.target.value)}>
              <option value="">全部节点</option>
              {nodeOptions.map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          </div>
          <Table
            head={["Tunnel", "节点", "回调地址", "Listener", "状态", "健康", "最近健康", "操作"]}
            rows={filteredIngress.map((v) => [
              String(v.tunnel_id),
              v.node_label || "—",
              v.callback_addr,
              "#" + v.listener_id + " " + v.listener_kind + " / " + v.listener_status,
              <Badge key={"is" + v.tunnel_id} variant={v.state === "alive" ? "default" : "secondary"}>{STATUS_LABEL[v.state] || v.state}</Badge>,
              <Badge key={"ih" + v.tunnel_id} variant={v.healthy ? "default" : "secondary"} title={v.last_error || ""}>{v.healthy ? "健康" : "异常"}</Badge>,
              fmtTime(v.last_health_at),
              <span key={"ia" + v.tunnel_id} className="flex justify-end gap-1">
                <Button size="sm" variant="outline" disabled={busy !== ""} onClick={() => void rebuildIngress(v.tunnel_id)}>重建</Button>
                <Button size="sm" variant="destructive" disabled={busy !== ""} onClick={() => void teardownIngress(v.tunnel_id)}>停止</Button>
              </span>,
            ])}
            empty="暂无入站中继"
          />
          {ingressEvents.length > 0 && (
            <div className="max-h-32 overflow-auto rounded-md border p-2 text-xs">
              <div className="mb-1 font-medium text-muted-foreground">最近事件</div>
              {ingressEvents.slice(0, 8).map((ev) => (
                <div key={ev.id} className="flex items-center gap-2 py-0.5">
                  <Badge variant={ev.level === "warn" ? "destructive" : "secondary"}>{ev.level}</Badge>
                  <span className="truncate">{ev.message}</span>
                  <span className="ml-auto shrink-0 text-muted-foreground">{fmtTime(ev.created_at)}</span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">④ 会话</CardTitle>
          <CardDescription>HTTP 植入端心跳自动上线；选中会话可下发命令，下次心跳领取。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end gap-2">
            <div className="min-w-48">
              <Label className="text-xs">会话</Label>
              <select
                className="border-input bg-background h-9 w-full rounded-md border px-2 text-sm"
                value={sessionId}
                onChange={(ev) => setSessionId(ev.target.value)}
              >
                <option value="">选择会话…</option>
                {sessions.map((s) => (
                  <option key={s.id} value={String(s.id)}>
                    #{s.id} {s.implant_uuid} ({s.remote_addr})
                  </option>
                ))}
              </select>
            </div>
            <div className="min-w-64 flex-1">
              <Label className="text-xs">命令</Label>
              <Input value={command} onChange={(ev) => setCommand(ev.target.value)} />
            </div>
            <Button size="sm" disabled={busy !== ""} onClick={() => void queueTask()}>
              下发任务
            </Button>
          </div>
          <Table
            head={["ID", "植入端", "地址", "OS / 用户", "状态", "最后心跳"]}
            rows={sessions.map((s) => [
              String(s.id),
              s.implant_uuid,
              s.remote_addr,
              (s.os || "—") + " / " + (s.username || "—"),
              <Badge key={"ss" + s.id} variant={s.status === "active" ? "default" : "secondary"}>
                {STATUS_LABEL[s.status] || s.status}
              </Badge>,
              fmtTime(s.last_check_in),
            ])}
            empty="暂无会话（等待植入端心跳）"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">⑤ 任务</CardTitle>
          <CardDescription>最新 50 条；排队任务由 HTTP 植入端心跳领取，结果回写 output/error。</CardDescription>
        </CardHeader>
        <CardContent>
          <Table
            head={["ID", "会话", "命令", "状态", "输出", "创建时间"]}
            rows={tasks.map((t) => [
              String(t.id),
              String(t.session_id),
              <span key={"cmd" + t.id} className="block max-w-64 truncate font-mono text-xs" title={t.args}>
                {t.args}
              </span>,
              <Badge key={"ts" + t.id} variant={t.status === "success" ? "default" : t.status === "failed" ? "destructive" : "secondary"}>
                {STATUS_LABEL[t.status] || t.status}
              </Badge>,
              <span key={"out" + t.id} className="block max-w-80 truncate font-mono text-xs" title={t.output || t.error || ""}>
                {e(t.output || t.error || "")}
              </span>,
              fmtTime(t.created_at),
            ])}
            empty="暂无任务"
          />
        </CardContent>
      </Card>
    </div>
  );
}

function Table({
  head,
  rows,
  empty,
}: {
  head: string[];
  rows: React.ReactNode[][];
  empty: string;
}) {
  if (rows.length === 0) {
    return <p className="text-muted-foreground text-sm">{empty}</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-muted-foreground text-left text-xs">
          <tr>
            {head.map((h) => (
              <th className="py-2" key={h}>
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr className="border-t" key={i}>
              {r.map((c, j) => (
                <td className="max-w-96 py-2" key={j}>
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
