"use client";

import * as React from "react";

import { Loader2Icon, NetworkIcon, RefreshCwIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type {
  IntranetCredential,
  IntranetHostOp,
  IntranetPendingScope,
  IntranetSession,
  IntranetTunnel,
  GeneratedWebshell,
  MihomoImportResult,
  ProxyPoolState,
  WebshellShell,
} from "@/lib/types";

type TabKey = "sessions" | "tunnels" | "credentials" | "ledger" | "scope" | "webshell" | "proxypool";

const TABS: { key: TabKey; label: string }[] = [
  { key: "sessions", label: "会话" },
  { key: "tunnels", label: "隧道" },
  { key: "credentials", label: "凭据" },
  { key: "ledger", label: "主机台账" },
  { key: "scope", label: "范围审批" },
  { key: "webshell", label: "WebShell" },
  { key: "proxypool", label: "代理池" },
];

function fmtTime(ts?: string) {
  if (!ts) return "-";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function statusBadge(status: string) {
  if (status === "alive") return <Badge>alive</Badge>;
  if (status === "dead" || status === "error" || status === "stopped")
    return <Badge variant="destructive">{status}</Badge>;
  return <Badge variant="secondary">{status}</Badge>;
}

export default function IntranetPage() {
  const [tab, setTab] = React.useState<TabKey>("sessions");
  const [taskFilter, setTaskFilter] = React.useState("");
  const [sessions, setSessions] = React.useState<IntranetSession[]>([]);
  const [tunnels, setTunnels] = React.useState<IntranetTunnel[]>([]);
  const [credentials, setCredentials] = React.useState<IntranetCredential[]>([]);
  const [ops, setOps] = React.useState<IntranetHostOp[]>([]);
  const [pending, setPending] = React.useState<IntranetPendingScope[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [handoff, setHandoff] = React.useState<IntranetSession | null>(null);
  const [handoffGoal, setHandoffGoal] = React.useState("");
  const [handoffDesc, setHandoffDesc] = React.useState("");
  const [handoffBusy, setHandoffBusy] = React.useState(false);
  const [notice, setNotice] = React.useState("");

  const taskID = React.useMemo(() => {
    const n = Number.parseInt(taskFilter.trim(), 10);
    return Number.isFinite(n) && n > 0 ? n : undefined;
  }, [taskFilter]);

  const load = React.useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      switch (tab) {
        case "sessions":
          setSessions((await api.intranetSessions(taskID)).sessions ?? []);
          break;
        case "tunnels":
          setTunnels((await api.intranetTunnels(taskID)).tunnels ?? []);
          break;
        case "credentials":
          setCredentials((await api.intranetCredentials(taskID)).credentials ?? []);
          break;
        case "ledger":
          setOps((await api.intranetHostLedger({ limit: 200 })).ops ?? []);
          break;
        case "scope":
          setPending((await api.intranetPendingScope(taskID)).requests ?? []);
          break;
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [tab, taskID]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: load already depends on tab/taskID.
  React.useEffect(() => {
    void load();
  }, [tab, taskID]);

  const approve = async (row: IntranetPendingScope) => {
    if (!window.confirm(`批准后将立即把 ${row.value} 加入任务 ${row.task_id} 的授权范围，后续工具调用即生效。确认批准？`)) return;
    try {
      await api.intranetApproveScope(row.id);
      setNotice(`已批准范围 ${row.value}`);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const reject = async (row: IntranetPendingScope) => {
    if (!window.confirm(`拒绝范围申请 ${row.value}？拒绝后 agent 无法对该网段继续操作。`)) return;
    try {
      await api.intranetRejectScope(row.id);
      setNotice(`已拒绝范围 ${row.value}`);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const submitHandoff = async () => {
    if (!handoff) return;
    if (!window.confirm(`以会话 #${handoff.id} 为起点创建子任务？将自动继承单主机授权范围。`)) return;
    setHandoffBusy(true);
    try {
      const r = await api.intranetHandoff({
        session_id: handoff.id,
        task_id: handoff.created_by_task,
        goal: handoffGoal.trim() || undefined,
        description: handoffDesc.trim() || undefined,
      });
      setNotice(`已创建子任务 #${r.task_id}${r.scope_error ? `（范围写入失败：${r.scope_error}）` : ""}`);
      setHandoff(null);
      setHandoffGoal("");
      setHandoffDesc("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setHandoffBusy(false);
    }
  };

  return (
    <div className="flex flex-1 flex-col gap-4 p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <NetworkIcon className="size-5" />
          <h1 className="font-semibold text-lg">内网中枢</h1>
          <span className="text-muted-foreground text-xs">立足点 / 隧道 / 凭据 / 台账 / 范围审批</span>
        </div>
        <div className="flex items-center gap-2">
          <Input
            className="w-40"
            placeholder="任务 ID 过滤"
            value={taskFilter}
            onChange={(e) => setTaskFilter(e.target.value)}
          />
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            {loading ? <Loader2Icon className="size-4 animate-spin" /> : <RefreshCwIcon className="size-4" />}
            刷新
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap gap-1">
        {TABS.map((t) => (
          <Button
            key={t.key}
            size="sm"
            variant={tab === t.key ? "default" : "ghost"}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </Button>
        ))}
      </div>

      {notice ? (
        <Card className="border-emerald-500/40 bg-emerald-500/5 p-3 text-sm">{notice}</Card>
      ) : null}
      {error ? (
        <Card className="border-destructive/40 bg-destructive/5 p-3 text-destructive text-sm">
          加载失败：{error}
        </Card>
      ) : null}

      <Card className="overflow-hidden">
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-10 text-muted-foreground text-sm">
            <Loader2Icon className="size-4 animate-spin" /> 加载中…
          </div>
        ) : tab === "sessions" ? (
          <>
            <SessionRegisterBlock onChanged={load} />
            <SessionsTable rows={sessions} onHandoff={(s) => setHandoff(s)} />
          </>
        ) : tab === "tunnels" ? (
          <TunnelsTable rows={tunnels} />
        ) : tab === "credentials" ? (
          <CredentialsBlock rows={credentials} taskID={taskID} onChanged={load} />
        ) : tab === "proxypool" ? (
          <ProxyPoolBlock />
        ) : tab === "webshell" ? (
          <WebshellBlock />
        ) : tab === "ledger" ? (
          <LedgerTable rows={ops} />
        ) : (
          <PendingScopeTable rows={pending} onApprove={approve} onReject={reject} />
        )}
      </Card>

      <Dialog open={handoff !== null} onOpenChange={(open) => !open && setHandoff(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>以会话 #{handoff?.id} 为起点建子任务</DialogTitle>
            <DialogDescription>
              子任务会继承该会话目标的主机范围（IP 为 /32），并通过来源任务与父任务关联。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Input
              placeholder="子任务目标（可选）"
              value={handoffGoal}
              onChange={(e) => setHandoffGoal(e.target.value)}
            />
            <Input
              placeholder="子任务描述（可选）"
              value={handoffDesc}
              onChange={(e) => setHandoffDesc(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setHandoff(null)} disabled={handoffBusy}>
              取消
            </Button>
            <Button onClick={() => void submitHandoff()} disabled={handoffBusy}>
              {handoffBusy ? <Loader2Icon className="size-4 animate-spin" /> : null}
              确认移交
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return <div className="p-10 text-center text-muted-foreground text-sm">{text}</div>;
}

function SessionRegisterBlock({ onChanged }: { onChanged: () => Promise<void> | void }) {
  const [kind, setKind] = React.useState("ssh");
  const [form, setForm] = React.useState({
    url: "",
    password: "",
    lang: "auto",
    host: "127.0.0.1",
    port: "22",
    user: "root",
    key: "",
    proxy: "",
    hash: "",
    proxychains: false,
  });
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      const body: Parameters<typeof api.intranetRegisterSession>[0] = { kind };
      if (kind === "webshell") {
        body.url = form.url.trim();
        body.password = form.password;
        body.lang = form.lang;
      } else if (kind === "ssh") {
        body.host = form.host.trim();
        body.port = Number.parseInt(form.port, 10) || 22;
        body.user = form.user.trim();
        body.password = form.password;
        body.key = form.key;
        body.proxy = form.proxy.trim();
      } else {
        body.host = form.host.trim();
        body.user = form.user.trim();
        body.password = form.password;
        body.hash = form.hash.trim();
        body.proxychains = form.proxychains;
      }
      const r = await api.intranetRegisterSession(body);
      setNotice("已登记 " + r.kind + " 会话 #" + r.session_id);
      setForm((f) => ({ ...f, password: "", key: "", hash: "" }));
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-2 border-b p-3">
      {error ? <p className="text-destructive text-sm">登记失败：{error}</p> : null}
      {notice ? <p className="text-emerald-600 text-sm">{notice}</p> : null}
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium text-sm">登记会话</span>
        <select
          className="rounded-md border bg-transparent px-2 py-1.5 text-sm"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          <option value="ssh">SSH（密码/私钥）</option>
          <option value="smbexec">SMB（wmiexec / NT hash）</option>
          <option value="webshell">WebShell</option>
        </select>
      </div>
      {kind === "webshell" ? (
        <div className="grid gap-2 md:grid-cols-3">
          <Input placeholder="webshell URL" value={form.url} onChange={set("url")} />
          <Input placeholder="连接密码" value={form.password} onChange={set("password")} />
          <Input placeholder="lang: auto/phpcmd/jsp/aspx" value={form.lang} onChange={set("lang")} />
        </div>
      ) : kind === "ssh" ? (
        <div className="grid gap-2 md:grid-cols-6">
          <Input placeholder="host" value={form.host} onChange={set("host")} />
          <Input placeholder="port" value={form.port} onChange={set("port")} />
          <Input placeholder="user" value={form.user} onChange={set("user")} />
          <Input placeholder="password" type="password" value={form.password} onChange={set("password")} />
          <Input placeholder="私钥 PEM（可选）" className="md:col-span-2" value={form.key} onChange={set("key")} />
          <Input placeholder="socks5 代理（可选）" className="md:col-span-2" value={form.proxy} onChange={set("proxy")} />
        </div>
      ) : (
        <div className="grid gap-2 md:grid-cols-5">
          <Input placeholder="host" value={form.host} onChange={set("host")} />
          <Input placeholder="DOMAIN\user" value={form.user} onChange={set("user")} />
          <Input placeholder="password" type="password" value={form.password} onChange={set("password")} />
          <Input placeholder="NT hash（pass-the-hash）" value={form.hash} onChange={set("hash")} />
          <label className="flex items-center gap-1 text-xs">
            <input
              type="checkbox"
              checked={form.proxychains}
              onChange={(e) => setForm((f) => ({ ...f, proxychains: e.target.checked }))}
            />
            proxychains
          </label>
        </div>
      )}
      <div>
        <Button size="sm" disabled={busy} onClick={() => void submit()}>
          {busy ? "登记中…" : "登记会话"}
        </Button>
      </div>
    </div>
  );
}

function SessionsTable({
  rows,
  onHandoff,
}: {
  rows: IntranetSession[];
  onHandoff: (row: IntranetSession) => void;
}) {
  if (rows.length === 0) return <EmptyState text="暂无会话。让 worker 用 register_session 登记 webshell 后会出现在这里。" />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-14">ID</TableHead>
          <TableHead>类型</TableHead>
          <TableHead>URL</TableHead>
          <TableHead className="w-20">状态</TableHead>
          <TableHead className="w-28">任务</TableHead>
          <TableHead className="w-36">最后心跳</TableHead>
          <TableHead className="w-24">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.id}>
            <TableCell>{r.id}</TableCell>
            <TableCell>
              <span className="font-mono text-xs">{r.kind}</span>
              {r.lang ? <span className="ml-1 text-muted-foreground text-xs">/{r.lang}</span> : null}
            </TableCell>
            <TableCell className="max-w-md truncate font-mono text-xs">{r.url}</TableCell>
            <TableCell>{statusBadge(r.status)}</TableCell>
            <TableCell>{r.created_by_task ?? "-"}</TableCell>
            <TableCell className="text-xs">{fmtTime(r.last_beat)}</TableCell>
            <TableCell>
              <Button size="sm" variant="outline" disabled={r.status !== "alive"} onClick={() => onHandoff(r)}>
                移交
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function TunnelsTable({ rows }: { rows: IntranetTunnel[] }) {
  if (rows.length === 0) return <EmptyState text="暂无隧道。worker 用 tunnel_deploy 部署后会出现在这里。" />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-14">ID</TableHead>
          <TableHead className="w-20">类型</TableHead>
          <TableHead className="w-24">适配器</TableHead>
          <TableHead className="w-20">状态</TableHead>
          <TableHead>入口</TableHead>
          <TableHead>目标</TableHead>
          <TableHead className="w-28">任务</TableHead>
          <TableHead>错误</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.id}>
            <TableCell>{r.id}</TableCell>
            <TableCell>{r.kind}</TableCell>
            <TableCell>{r.adapter}</TableCell>
            <TableCell>{statusBadge(r.state)}</TableCell>
            <TableCell className="font-mono text-xs">
              {r.listen_host || "0.0.0.0"}:{r.listen_port}
            </TableCell>
            <TableCell className="font-mono text-xs">
              {r.target_host ? `${r.target_host}:${r.target_port ?? ""}` : "-"}
            </TableCell>
            <TableCell>{r.task_id}</TableCell>
            <TableCell className="max-w-xs truncate text-destructive text-xs">{r.error || "-"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function CredentialsBlock({
  rows,
  taskID,
  onChanged,
}: {
  rows: IntranetCredential[];
  taskID?: number;
  onChanged: () => Promise<void> | void;
}) {
  const [form, setForm] = React.useState({
    username: "",
    cred_type: "password",
    secret: "",
    domain: "",
    task_id: taskID ? String(taskID) : "",
  });
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [revealed, setRevealed] = React.useState<{ id: number; username: string; secret: string } | null>(null);

  const create = async () => {
    const tid = Number.parseInt(form.task_id, 10);
    if (!Number.isFinite(tid) || tid <= 0) {
      setError("任务 ID 必填（凭据按任务归属）");
      return;
    }
    if (!form.username.trim() || !form.secret.trim()) {
      setError("用户名与密码/机密必填");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const r = await api.intranetCreateCredential({
        task_id: tid,
        username: form.username.trim(),
        cred_type: form.cred_type,
        secret: form.secret,
        domain: form.domain.trim(),
      });
      setNotice("已登记凭据 #" + r.id);
      setForm({ ...form, username: "", secret: "", domain: "" });
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const reveal = async (id: number) => {
    setBusy(true);
    setError("");
    try {
      const r = await api.intranetRevealCredential(id);
      setRevealed({ id: r.id, username: r.username, secret: r.secret });
      setNotice("已读取凭据 #" + id + " 明文（本次读取已写服务端日志）");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    if (!window.confirm("删除凭据 #" + id + "？删除后无法恢复。")) return;
    setBusy(true);
    setError("");
    try {
      await api.intranetDeleteCredential(id);
      setNotice("已删除凭据 #" + id);
      setRevealed(null);
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-3 p-3">
      {error ? <Card className="border-destructive/40 bg-destructive/5 p-2 text-destructive text-sm">{error}</Card> : null}
      {notice ? <Card className="border-emerald-500/40 bg-emerald-500/5 p-2 text-emerald-600 text-sm">{notice}</Card> : null}
      <div className="flex flex-wrap items-end gap-2">
        <Input className="w-24" placeholder="任务 ID" value={form.task_id} onChange={(e) => setForm({ ...form, task_id: e.target.value })} />
        <Input className="w-36" placeholder="用户名" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
        <select
          className="rounded-md border bg-transparent px-2 py-2 text-sm"
          value={form.cred_type}
          onChange={(e) => setForm({ ...form, cred_type: e.target.value })}
        >
          <option value="password">password</option>
          <option value="hash_nt">hash_nt</option>
          <option value="hash_lm">hash_lm</option>
          <option value="hash_sha1">hash_sha1</option>
          <option value="hash_unix">hash_unix</option>
          <option value="ssh_key">ssh_key</option>
          <option value="ticket">ticket</option>
          <option value="token">token</option>
        </select>
        <Input className="w-48" placeholder="密码 / 机密" value={form.secret} onChange={(e) => setForm({ ...form, secret: e.target.value })} />
        <Input className="w-28" placeholder="域（可选）" value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} />
        <Button size="sm" disabled={busy} onClick={() => void create()}>
          新增凭据
        </Button>
      </div>
      {revealed ? (
        <Card className="flex flex-wrap items-center gap-2 border-amber-500/40 bg-amber-500/5 p-2 text-sm">
          <span className="font-medium">#{revealed.id} {revealed.username} 明文：</span>
          <code className="max-w-full break-all font-mono text-xs">{revealed.secret}</code>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              navigator.clipboard.writeText(revealed.secret).then(
                () => setNotice("已复制到剪贴板"),
                () => setError("复制失败"),
              );
            }}
          >
            复制
          </Button>
        </Card>
      ) : null}
      {rows.length === 0 ? (
        <p className="p-4 text-center text-muted-foreground text-sm">暂无凭据。agent 登记或手动新增后会出现在这里。</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-14">ID</TableHead>
              <TableHead className="w-20">任务</TableHead>
              <TableHead>用户名</TableHead>
              <TableHead className="w-24">类型</TableHead>
              <TableHead className="w-24">域</TableHead>
              <TableHead>密码/机密</TableHead>
              <TableHead className="w-20">已验证</TableHead>
              <TableHead>来源</TableHead>
              <TableHead className="w-44">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.id}>
                <TableCell>{r.id}</TableCell>
                <TableCell>{r.task_id}</TableCell>
                <TableCell className="font-mono text-xs">{r.username || "-"}</TableCell>
                <TableCell>{r.cred_type}</TableCell>
                <TableCell>{r.domain || "-"}</TableCell>
                <TableCell className="font-mono text-xs">
                  {r.secret_ok ? r.secret_masked || "******" : <span className="text-destructive">解密失败</span>}
                </TableCell>
                <TableCell>{r.verified ? <Badge>已验</Badge> : <Badge variant="secondary">未验</Badge>}</TableCell>
                <TableCell className="max-w-xs truncate text-xs">{r.source || "-"}</TableCell>
                <TableCell className="flex gap-1">
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => void reveal(r.id)}>
                    显示
                  </Button>
                  <Button size="sm" variant="destructive" disabled={busy} onClick={() => void remove(r.id)}>
                    删除
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function LedgerTable({ rows }: { rows: IntranetHostOp[] }) {
  if (rows.length === 0) return <EmptyState text="暂无主机操作记录。会话执行/读写会自动记账。" />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-14">ID</TableHead>
          <TableHead className="w-20">会话</TableHead>
          <TableHead className="w-32">主机</TableHead>
          <TableHead className="w-20">通道</TableHead>
          <TableHead>命令</TableHead>
          <TableHead className="w-16">结果</TableHead>
          <TableHead className="w-24">痕迹</TableHead>
          <TableHead className="w-32">时间</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((op) => (
          <TableRow key={op.id}>
            <TableCell>{op.id}</TableCell>
            <TableCell>{op.session_id || "-"}</TableCell>
            <TableCell className="font-mono text-xs">{op.host || "-"}</TableCell>
            <TableCell>{op.channel}</TableCell>
            <TableCell className="max-w-xl truncate font-mono text-xs">{op.cmd}</TableCell>
            <TableCell>{op.ok ? <Badge>ok</Badge> : <Badge variant="destructive">fail</Badge>}</TableCell>
            <TableCell>
              {op.artifact ? <Badge variant="secondary">{op.artifact_type || "artifact"}</Badge> : "-"}
            </TableCell>
            <TableCell className="text-xs">{fmtTime(op.created_at)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function PendingScopeTable({
  rows,
  onApprove,
  onReject,
}: {
  rows: IntranetPendingScope[];
  onApprove: (row: IntranetPendingScope) => void;
  onReject: (row: IntranetPendingScope) => void;
}) {
  if (rows.length === 0) return <EmptyState text="暂无待审批范围。agent 发现越界网段时会在这里申请。" />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-14">ID</TableHead>
          <TableHead className="w-28">任务</TableHead>
          <TableHead className="w-20">类型</TableHead>
          <TableHead>范围</TableHead>
          <TableHead>申请原因</TableHead>
          <TableHead className="w-20">来源</TableHead>
          <TableHead className="w-40">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.id}>
            <TableCell>{r.id}</TableCell>
            <TableCell>{r.task_id}</TableCell>
            <TableCell>{r.kind}</TableCell>
            <TableCell className="font-mono text-xs">{r.value}</TableCell>
            <TableCell className="max-w-md truncate text-xs">{r.reason || "-"}</TableCell>
            <TableCell>{r.requested_by || "-"}</TableCell>
            <TableCell className="flex gap-2">
              <Button size="sm" onClick={() => onApprove(r)}>
                批准
              </Button>
              <Button size="sm" variant="destructive" onClick={() => onReject(r)}>
                拒绝
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function EmptyPlaceholder() {
  return null;
}
// ---------------- WebShell（M4-02）：生成 / 登记 / 执行 / 读取 / 清理 ----------------

function WebshellBlock() {
  const [shells, setShells] = React.useState<WebshellShell[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [gen, setGen] = React.useState<GeneratedWebshell | null>(null);
  const [genLang, setGenLang] = React.useState("php");
  const [obfuscate, setObfuscate] = React.useState(false);
  const [regForm, setRegForm] = React.useState({ url: "", password: "cmd", lang: "auto", secret: "", path: "" });
  const [selected, setSelected] = React.useState<number | null>(null);
  const [command, setCommand] = React.useState("id");
  const [execOut, setExecOut] = React.useState("");
  const [readPath, setReadPath] = React.useState("/etc/passwd");
  const [busy, setBusy] = React.useState(false);

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      setShells((await api.webshellShells()).shells ?? []);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  const generate = async () => {
    setBusy(true);
    try {
      const g = await api.webshellGenerate({ lang: genLang, obfuscate });
      setGen(g);
      setNotice(`已生成 ${g.lang}（${g.filename}），密钥只显示这一次`);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const register = async () => {
    setBusy(true);
    try {
      const r = await api.webshellRegister({
        url: regForm.url, password: regForm.password || undefined,
        lang: regForm.lang, secret: regForm.secret || undefined,
        path: regForm.path.trim() || undefined,
      });
      setNotice(`已登记会话 #${r.session_id}（${r.kind}）`);
      setError("");
      setSelected(r.session_id);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const run = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      const r = await api.webshellExec({ session_id: selected, command });
      setExecOut(r.stdout || r.stderr || "（无输出）");
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const readFile = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      const r = await api.webshellRead({ session_id: selected, path: readPath });
      setExecOut(r.text ?? `size=${r.size} truncated=${r.truncated}`);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const cleanup = async (id: number) => {
    if (!window.confirm("清理该 WebShell？清理动作会写入 host_ledger，会话记录将被删除。")) return;
    setBusy(true);
    try {
      const r = await api.webshellCleanup(id);
      setNotice(r.physical ? r.cleanup + (r.verified ? "（已校验文件不存在）" : "") : r.cleanup);
      setSelected(null);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {error ? <Card className="border-destructive/40 bg-destructive/5 p-3 text-destructive text-sm">{error}</Card> : null}
      {notice ? <Card className="border-emerald-500/40 bg-emerald-500/5 p-3 text-emerald-600 text-sm">{notice}</Card> : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex flex-col gap-2 p-3">
          <p className="font-medium text-sm">生成加密马（平台侧渲染）</p>
          <div className="flex gap-2">
            <select className="rounded-md border bg-transparent px-2 text-sm" value={genLang} onChange={(e) => setGenLang(e.target.value)}>
              <option value="php">php</option>
              <option value="jsp">jsp</option>
              <option value="aspx">aspx</option>
            </select>
            <label className="flex items-center gap-1 text-xs">
              <input type="checkbox" checked={obfuscate} onChange={(e) => setObfuscate(e.target.checked)} />
              轻度混淆
            </label>
            <Button size="sm" disabled={busy} onClick={() => void generate()}>
              生成
            </Button>
          </div>
          {gen ? (
            <div className="flex flex-col gap-1 text-xs">
              <p className="text-muted-foreground">
                文件名 {gen.filename} · 模式 {gen.mode} · 密钥 sha256 {gen.key_digest}
              </p>
              <textarea className="min-h-20 rounded border p-2 font-mono text-xs" readOnly value={gen.key_b64} />
              <textarea className="min-h-32 rounded border p-2 font-mono text-xs" readOnly value={gen.content} />
              {gen.register_hint ? <p className="text-muted-foreground">{gen.register_hint}</p> : null}
            </div>
          ) : null}
        </Card>

        <Card className="flex flex-col gap-2 p-3">
          <p className="font-medium text-sm">登记已有 WebShell</p>
          <Input placeholder="完整 URL（含协议）" value={regForm.url} onChange={(e) => setRegForm({ ...regForm, url: e.target.value })} />
          <div className="grid grid-cols-3 gap-2">
            <Input placeholder="password" value={regForm.password} onChange={(e) => setRegForm({ ...regForm, password: e.target.value })} />
            <Input placeholder="lang: auto/php/phpcmd/jsp/aspx/phpenc..." value={regForm.lang} onChange={(e) => setRegForm({ ...regForm, lang: e.target.value })} />
            <Input placeholder="加密马 secret" value={regForm.secret} onChange={(e) => setRegForm({ ...regForm, secret: e.target.value })} />
          </div>
          <Input placeholder="目标侧绝对路径（可选，用于物理删除+校验，如 /var/www/html/x.php）" value={regForm.path} onChange={(e) => setRegForm({ ...regForm, path: e.target.value })} />
          <Button size="sm" disabled={busy || !regForm.url.trim()} onClick={() => void register()}>
            登记（先探测再落库）
          </Button>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <div className="flex items-center justify-between p-2">
          <p className="font-medium text-sm">已登记 WebShell（{shells.length}）</p>
          <Button size="sm" variant="ghost" onClick={() => void refresh()}>
            <RefreshCwIcon className="size-4" /> 刷新
          </Button>
        </div>
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-8 text-muted-foreground text-sm">
            <Loader2Icon className="size-4 animate-spin" /> 加载中…
          </div>
        ) : shells.length === 0 ? (
          <div className="p-8 text-center text-muted-foreground text-sm">暂无 WebShell 会话</div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-16">ID</TableHead>
                <TableHead>host</TableHead>
                <TableHead>目标路径</TableHead>
                <TableHead className="w-24">kind</TableHead>
                <TableHead className="w-20">状态</TableHead>
                <TableHead className="w-32">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {shells.map((sh) => (
                <TableRow key={sh.id} className={selected === sh.id ? "bg-muted/50" : ""}>
                  <TableCell>{sh.id}</TableCell>
                  <TableCell className="max-w-md truncate">{sh.host}</TableCell>
                  <TableCell className="max-w-xs truncate font-mono text-xs">{sh.artifact_path || "-"}</TableCell>
                  <TableCell className="text-xs">{sh.kind}</TableCell>
                  <TableCell>
                    <Badge variant={sh.status === "alive" ? "default" : "secondary"}>{sh.status}</Badge>
                  </TableCell>
                  <TableCell className="flex gap-1">
                    <Button size="sm" variant="ghost" onClick={() => setSelected(sh.id)}>
                      选中
                    </Button>
                    <Button size="sm" variant="destructive" onClick={() => void cleanup(sh.id)}>
                      清理
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <Card className="flex flex-col gap-2 p-3">
        <p className="font-medium text-sm">执行 / 读取（选中会话 #{selected ?? "-"}）</p>
        <div className="flex gap-2">
          <Input placeholder="shell 命令" value={command} onChange={(e) => setCommand(e.target.value)} />
          <Button size="sm" disabled={busy || !selected} onClick={() => void run()}>执行</Button>
        </div>
        <div className="flex gap-2">
          <Input placeholder="目标文件路径" value={readPath} onChange={(e) => setReadPath(e.target.value)} />
          <Button size="sm" variant="outline" disabled={busy || !selected} onClick={() => void readFile()}>读取</Button>
        </div>
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-2 text-xs">{execOut}</pre>
      </Card>
    </div>
  );
}

// ---------------- 代理池（M4-03）：录入 / 探测 / 切换 / 回退 ----------------

function ProxyPoolBlock() {
  const [state, setState] = React.useState<ProxyPoolState | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [form, setForm] = React.useState({ name: "", url: "" });
  const [globalURL, setGlobalURL] = React.useState("");
  const [egress, setEgress] = React.useState("");
  const [mihomoYAML, setMihomoYAML] = React.useState("");
  const [mihomoPrefix, setMihomoPrefix] = React.useState("mh-");
  const [importing, setImporting] = React.useState(false);
  const [importResult, setImportResult] = React.useState<MihomoImportResult | null>(null);

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      setState(await api.proxyPool());
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {error ? <Card className="border-destructive/40 bg-destructive/5 p-3 text-destructive text-sm">{error}</Card> : null}
      {notice ? <Card className="border-emerald-500/40 bg-emerald-500/5 p-3 text-emerald-600 text-sm">{notice}</Card> : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex flex-col gap-2 p-3">
          <p className="font-medium text-sm">录入代理（http/https/socks5，支持认证）</p>
          <div className="flex gap-2">
            <Input placeholder="名称（可选）" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            <Input placeholder="http://user:pass@host:port" value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} />
            <Button size="sm" disabled={busy || !form.url.trim()} onClick={() => void run(async () => {
              const r = await api.proxyPoolAdd({ url: form.url, name: form.name });
              const bad = r.results.find((x) => x.error);
              setNotice(bad ? `录入失败：${bad.error}` : "录入成功（重复项自动去重）");
              setForm({ name: "", url: "" });
              await refresh();
            })}>录入</Button>
          </div>
        </Card>
        <Card className="flex flex-col gap-2 p-3">
          <p className="font-medium text-sm">全局兜底代理（活动代理失败时自动回退）</p>
          <div className="flex gap-2">
            <Input placeholder="留空 = 直连" value={globalURL} onChange={(e) => setGlobalURL(e.target.value)} />
            <Button size="sm" variant="outline" disabled={busy} onClick={() => void run(async () => {
              await api.proxyPoolGlobal(globalURL);
              setNotice(globalURL ? "全局兜底代理已保存" : "已清除全局兜底代理");
              await refresh();
            })}>保存</Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(async () => {
              const r = await api.proxyPoolFallback();
              setNotice(r.global_set ? "已切回兜底代理" : "已切回直连");
              await refresh();
            })}>切回兜底</Button>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="secondary" disabled={busy} onClick={() => void run(async () => {
              const r = await api.proxyPoolEgress();
              setEgress(`出口 IP：${r.exit_ip ?? "-"}（活动代理 ${r.active_id ?? 0}）`);
            })}>出口自检</Button>
            <span className="text-muted-foreground text-xs">{egress}</span>
          </div>
        </Card>
      </div>

      <Card className="flex flex-col gap-2 p-3">
        <p className="font-medium text-sm">从 mihomo / Clash 配置导入（适配器：仅 http/socks5）</p>
        <textarea
          className="min-h-28 rounded-md border bg-transparent p-2 font-mono text-xs"
          placeholder="粘贴 mihomo config.yaml 或订阅内容（proxies 列表）"
          value={mihomoYAML}
          onChange={(e) => setMihomoYAML(e.target.value)}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Input className="w-32" placeholder="名称前缀" value={mihomoPrefix} onChange={(e) => setMihomoPrefix(e.target.value)} />
          <Button
            size="sm"
            disabled={busy || importing || !mihomoYAML.trim()}
            onClick={() => {
              setImporting(true);
              void run(async () => {
                try {
                  const r = await api.proxyPoolImportMihomo({ yaml: mihomoYAML, name_prefix: mihomoPrefix });
                  setImportResult(r);
                  setNotice(
                    "导入完成：新增 " + r.imported_count + " 条，重复 " + r.duplicates +
                    " 条，跳过 " + r.skipped.length + " 条（ss/vmess 等需 mihomo 内核转发）",
                  );
                  setMihomoYAML("");
                  await refresh();
                } finally {
                  setImporting(false);
                }
              });
            }}
          >
            {importing ? "导入中…" : "导入代理"}
          </Button>
          <span className="text-muted-foreground text-xs">
            认证代理自动加密落库；跳过项见下方结果。
          </span>
        </div>
        {importResult ? (
          <div className="flex flex-col gap-1 rounded border bg-muted/30 p-2 text-xs">
            <span>
              新增 {importResult.imported_count} · 重复 {importResult.duplicates} · 跳过 {importResult.skipped.length}
            </span>
            {importResult.skipped.map((x, i) => (
              <span key={i} className="text-muted-foreground">
                跳过 {x.name || "(未命名)"}（{x.type || "?"}）：{x.reason}
              </span>
            ))}
            {importResult.errors.map((x, i) => (
              <span key={i} className="text-destructive">
                失败 {x.name}：{x.error}
              </span>
            ))}
          </div>
        ) : null}
      </Card>


      <Card className="overflow-hidden">
        <div className="flex items-center justify-between p-2">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <p className="font-medium text-sm">代理池（{state?.entries.length ?? 0}）</p>
            <Badge variant="secondary">健康 {state?.healthy ?? 0}</Badge>
            <Badge variant={(state?.failed ?? 0) > 0 ? "destructive" : "secondary"}>失败 {state?.failed ?? 0}</Badge>
            <span className="text-muted-foreground">
              活动 #{state?.active_id || 0} · 探测地址 {state?.probe_url || "-"} · 统一出站：
              {(state?.unified_outbound ?? []).join(" / ") || "-"}
            </span>
          </div>
          <Button size="sm" variant="ghost" onClick={() => void refresh()}>
            <RefreshCwIcon className="size-4" /> 刷新
          </Button>
        </div>
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-8 text-muted-foreground text-sm">
            <Loader2Icon className="size-4 animate-spin" /> 加载中…
          </div>
        ) : (state?.entries.length ?? 0) === 0 ? (
          <div className="p-8 text-center text-muted-foreground text-sm">代理池为空</div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-16">ID</TableHead>
                <TableHead>地址</TableHead>
                <TableHead className="w-20">认证</TableHead>
                <TableHead className="w-24">状态</TableHead>
                <TableHead className="w-32">出口 IP</TableHead>
                <TableHead className="w-20">延迟</TableHead>
                <TableHead className="w-16">失败</TableHead>
                <TableHead className="w-40">最近检查</TableHead>
                <TableHead className="w-52">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(state?.entries ?? []).map((e) => (
                <TableRow key={e.id}>
                  <TableCell>{e.id}</TableCell>
                  <TableCell className="max-w-md truncate">
                    {e.name ? `${e.name} · ` : ""}{e.masked}
                    {state?.active_id === e.id ? <Badge className="ml-2">活动</Badge> : null}
                  </TableCell>
                  <TableCell>{e.auth ? <Badge variant="secondary">已认证</Badge> : <span className="text-muted-foreground text-xs">无</span>}</TableCell>
                  <TableCell>
                    <Badge variant={e.status === "active" ? "default" : e.status === "failed" ? "destructive" : "secondary"}>
                      {e.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs">{e.exit_ip || "-"}</TableCell>
                  <TableCell className="text-xs">{e.latency_ms ? `${e.latency_ms}ms` : "-"}</TableCell>
                  <TableCell className="text-xs">{e.fail_count}</TableCell>
                  <TableCell className="text-xs" title={e.last_error || ""}>
                    {e.last_checked && new Date(e.last_checked).getFullYear() > 1970
                      ? new Date(e.last_checked).toLocaleString("zh-CN", { hour12: false })
                      : "-"}
                  </TableCell>
                  <TableCell className="flex gap-1">
                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(async () => {
                      const r = await api.proxyPoolProbe(e.id);
                      setNotice(r.ok ? `探测成功：出口 ${r.exit_ip}（${r.latency_ms}ms）` : `探测失败：${r.error}`);
                      await refresh();
                    })}>探测</Button>
                    <Button size="sm" variant="ghost" disabled={busy || state?.active_id === e.id} onClick={() => void run(async () => {
                      await api.proxyPoolSwitch(e.id);
                      setNotice(`已切换到代理 #${e.id}，新请求将走新出口`);
                      await refresh();
                    })}>切换</Button>
                    <Button size="sm" variant="destructive" disabled={busy} onClick={() => void run(async () => {
                      if (!window.confirm(`删除代理 #${e.id}？`)) return;
                      await api.proxyPoolDelete(e.id);
                      await refresh();
                    })}>删除</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}
