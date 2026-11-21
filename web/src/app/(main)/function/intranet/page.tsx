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
} from "@/lib/types";

type TabKey = "sessions" | "tunnels" | "credentials" | "ledger" | "scope";

const TABS: { key: TabKey; label: string }[] = [
  { key: "sessions", label: "会话" },
  { key: "tunnels", label: "隧道" },
  { key: "credentials", label: "凭据" },
  { key: "ledger", label: "主机台账" },
  { key: "scope", label: "范围审批" },
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
          <SessionsTable rows={sessions} onHandoff={(s) => setHandoff(s)} />
        ) : tab === "tunnels" ? (
          <TunnelsTable rows={tunnels} />
        ) : tab === "credentials" ? (
          <CredentialsTable rows={credentials} />
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

function CredentialsTable({ rows }: { rows: IntranetCredential[] }) {
  if (rows.length === 0) return <EmptyState text="暂无凭据。agent 登记密码/哈希/密钥后会出现在这里。" />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-14">ID</TableHead>
          <TableHead className="w-28">任务</TableHead>
          <TableHead>用户名</TableHead>
          <TableHead className="w-24">类型</TableHead>
          <TableHead className="w-28">域</TableHead>
          <TableHead>密码/机密</TableHead>
          <TableHead className="w-20">已验证</TableHead>
          <TableHead>来源</TableHead>
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
              {r.secret_ok ? (r.secret_masked || "******") : <span className="text-destructive">解密失败</span>}
            </TableCell>
            <TableCell>{r.verified ? <Badge>已验</Badge> : <Badge variant="secondary">未验</Badge>}</TableCell>
            <TableCell className="max-w-xs truncate text-xs">{r.source || "-"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
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