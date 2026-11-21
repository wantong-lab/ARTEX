"use client";

import { useCallback, useEffect, useState } from "react";

import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { C2Listener, C2Session, C2Task } from "@/lib/types";

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

  const refresh = useCallback(async () => {
    const [st, ls, ss, ts] = await Promise.all([
      api.c2Status(),
      api.c2Listeners(),
      api.c2Sessions(),
      api.c2Tasks(),
    ]);
    setEnabled(st.enabled);
    setListeners(ls.listeners ?? []);
    setSessions(ss.sessions ?? []);
    setTasks(ts.tasks ?? []);
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

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">监听器</CardTitle>
            <CardDescription>TCP 长连接 / HTTP beacon 两种形态；token 加密落库，仅创建时生成。</CardDescription>
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
            <CardTitle className="text-base">植入端生成</CardTitle>
            <CardDescription>仅 HTTP 监听器支持；token 内嵌在脚本中，请妥善保管。</CardDescription>
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
          <CardTitle className="text-base">会话</CardTitle>
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
          <CardTitle className="text-base">任务</CardTitle>
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
