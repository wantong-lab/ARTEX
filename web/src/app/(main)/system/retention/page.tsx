"use client";

import { useCallback, useEffect, useState } from "react";

import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type {
  RetentionCleanupList,
  RetentionCleanupResult,
  RetentionPolicy,
  RetentionStatus,
  SchedulerStatus,
} from "@/lib/types";

const DEFAULT_POLICY: RetentionPolicy = { activity_days: 30, logs_days: 14, c2_days: 30 };

function fmtUnix(v?: number): string {
  if (!v) return "—";
  return new Date(v * 1000).toLocaleString("zh-CN", { hour12: false });
}

function fmtTime(v?: string): string {
  if (!v) return "—";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString("zh-CN", { hour12: false });
}

function countsText(c?: RetentionCleanupResult["counts"]): string {
  if (!c) return "—";
  return (
    "activity " +
    c.activity +
    " · 日志 " +
    c.server_logs +
    " · C2任务 " +
    c.c2_tasks +
    " · C2事件 " +
    c.c2_events +
    " · C2会话 " +
    c.c2_sessions
  );
}

function intervalText(sec?: number): string {
  if (!sec) return "—";
  if (sec % 3600 === 0) return sec / 3600 + " 小时";
  return Math.round(sec / 60) + " 分钟";
}

const JOB_LABELS: Record<string, string> = {
  retention: "保留清理",
  cve_autosync: "CVE 自动同步",
  proxy_health: "代理池巡检",
};

export default function RetentionPage() {
  const [status, setStatus] = useState<RetentionStatus | null>(null);
  const [cleanup, setCleanup] = useState<RetentionCleanupList | null>(null);
  const [scheduler, setScheduler] = useState<SchedulerStatus | null>(null);
  const [policy, setPolicy] = useState<RetentionPolicy>(DEFAULT_POLICY);
  const [busy, setBusy] = useState("");
  const [dryRunResult, setDryRunResult] = useState<RetentionCleanupResult | null>(null);
  const [lastResult, setLastResult] = useState<RetentionCleanupResult | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [loaded, setLoaded] = useState(false);

  const refresh = useCallback(async () => {
    const [retention, list, sched] = await Promise.all([
      api.retentionStatus(),
      api.retentionCleanupList(),
      api.schedulerStatus().catch(() => null),
    ]);
    setStatus(retention);
    setPolicy(retention.policy);
    setCleanup(list);
    setScheduler(sched);
    setLoaded(true);
  }, []);

  useEffect(() => {
    refresh().catch((e: Error) => toast.error("加载保留策略失败：" + e.message));
  }, [refresh]);

  async function savePolicy() {
    setBusy("save");
    try {
      const r = await api.retentionSavePolicy(policy);
      setPolicy(r.policy);
      toast.success("保留策略已保存");
      await refresh();
    } catch (e) {
      toast.error("保存失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function runDryRun() {
    setBusy("dry");
    try {
      const r = await api.retentionCleanup(true, "web");
      setDryRunResult(r);
      setConfirming(false);
      toast.success("试运行完成：不会删除任何数据");
      await refresh();
    } catch (e) {
      toast.error("试运行失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function runCleanup() {
    setBusy("real");
    try {
      const r = await api.retentionCleanup(false, "web");
      setLastResult(r);
      setConfirming(false);
      toast.success("清理完成：" + countsText(r.deleted));
      await refresh();
    } catch (e) {
      toast.error("清理失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function runSchedulerJob(name: string) {
    setBusy("job:" + name);
    try {
      const r = await api.schedulerRun(name);
      toast.success((JOB_LABELS[name] || name) + "：" + r.result);
      await refresh();
    } catch (e) {
      toast.error("执行失败：" + (e as Error).message);
    } finally {
      setBusy("");
    }
  }

  const preview = status?.preview;
  const cleanupItems = cleanup?.items ?? [];
  const hosts = cleanup?.by_host ?? {};

  return (
    <div className="flex flex-col gap-4 px-4 lg:px-6">
      <header className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">数据保留</h1>
          <p className="text-muted-foreground text-sm">
            到期数据清理、调度状态与目标侧清理清单；真实清理会写入审计。
          </p>
        </div>
        <Button variant="outline" size="sm" disabled={!loaded || busy !== ""} onClick={() => void refresh()}>
          刷新
        </Button>
      </header>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">保留策略</CardTitle>
            <CardDescription>单位：天。策略保存后对调度器与手动清理同时生效，范围 1–3650。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1.5">
                <Label htmlFor="activity-days">activity 保留</Label>
                <Input
                  id="activity-days"
                  type="number"
                  min={1}
                  max={3650}
                  value={policy.activity_days}
                  onChange={(e) => setPolicy({ ...policy, activity_days: Number(e.target.value) })}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="logs-days">服务日志保留</Label>
                <Input
                  id="logs-days"
                  type="number"
                  min={1}
                  max={3650}
                  value={policy.logs_days}
                  onChange={(e) => setPolicy({ ...policy, logs_days: Number(e.target.value) })}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="c2-days">C2 台账保留</Label>
                <Input
                  id="c2-days"
                  type="number"
                  min={1}
                  max={3650}
                  value={policy.c2_days}
                  onChange={(e) => setPolicy({ ...policy, c2_days: Number(e.target.value) })}
                />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button size="sm" disabled={busy !== ""} onClick={() => void savePolicy()}>
                {busy === "save" ? "保存中…" : "保存策略"}
              </Button>
              <Button variant="outline" size="sm" disabled={busy !== ""} onClick={() => void runDryRun()}>
                {busy === "dry" ? "试运行中…" : "试运行（不删除）"}
              </Button>
              {confirming ? (
                <div className="flex items-center gap-2 rounded-lg border border-destructive/40 bg-destructive/5 px-2 py-1">
                  <span className="text-destructive text-xs">确认立即删除到期数据？</span>
                  <Button size="sm" variant="destructive" disabled={busy !== ""} onClick={() => void runCleanup()}>
                    {busy === "real" ? "清理中…" : "确认清理"}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                    取消
                  </Button>
                </div>
              ) : (
                <Button variant="destructive" size="sm" disabled={busy !== ""} onClick={() => setConfirming(true)}>
                  立即清理
                </Button>
              )}
            </div>
            {dryRunResult ? (
              <div className="rounded-lg border bg-muted/40 p-2 text-xs">
                <span className="font-medium">试运行结果（不会删除）：</span> {countsText(dryRunResult.counts)}
              </div>
            ) : null}
            {lastResult ? (
              <div className="rounded-lg border bg-muted/40 p-2 text-xs">
                <span className="font-medium">最近一次清理：</span> 删除 {countsText(lastResult.deleted)}；审计 #
                {lastResult.audit_id ?? "—"}
              </div>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">到期预览与最近审计</CardTitle>
            <CardDescription>预览只统计到期条数；真实清理后会写入 retention_audit。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              <div className="rounded-lg border p-2">
                <div className="text-muted-foreground text-xs">activity</div>
                <div className="text-lg font-semibold">{preview?.activity ?? 0}</div>
              </div>
              <div className="rounded-lg border p-2">
                <div className="text-muted-foreground text-xs">服务日志</div>
                <div className="text-lg font-semibold">{preview?.server_logs ?? 0}</div>
              </div>
              <div className="rounded-lg border p-2">
                <div className="text-muted-foreground text-xs">C2 任务</div>
                <div className="text-lg font-semibold">{preview?.c2_tasks ?? 0}</div>
              </div>
              <div className="rounded-lg border p-2">
                <div className="text-muted-foreground text-xs">C2 事件</div>
                <div className="text-lg font-semibold">{preview?.c2_events ?? 0}</div>
              </div>
              <div className="rounded-lg border p-2">
                <div className="text-muted-foreground text-xs">C2 会话</div>
                <div className="text-lg font-semibold">{preview?.c2_sessions ?? 0}</div>
              </div>
            </div>
            <div className="text-muted-foreground text-xs">
              最近审计：
              {status?.last_cleanup
                ? "#" + status.last_cleanup.id + " · " + status.last_cleanup.actor + " · " + fmtTime(status.last_cleanup.at) + " · 删除 " + countsText(status.last_cleanup.deleted)
                : "暂无"}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">定时调度状态</CardTitle>
          <CardDescription>
            维护性调度（保留清理 / CVE 自动同步 / 代理池巡检）；支持手动立即执行，状态由 /api/scheduler/status 提供。
          </CardDescription>
        </CardHeader>
        <CardContent>
          {scheduler ? (
            <table className="w-full text-sm">
              <thead className="text-muted-foreground text-left text-xs">
                <tr>
                  <th className="py-2">任务</th>
                  <th className="py-2">启用</th>
                  <th className="py-2">周期</th>
                  <th className="py-2">下次运行</th>
                  <th className="py-2">上次运行</th>
                  <th className="py-2">最近结果</th>
                  <th className="py-2 text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {scheduler.jobs.map((job) => (
                  <tr className="border-t" key={job.name}>
                    <td className="py-2 font-medium">{JOB_LABELS[job.name] || job.name}</td>
                    <td className="py-2">
                      <Badge variant={job.enabled ? "default" : "secondary"}>{job.enabled ? "启用" : "停用"}</Badge>
                    </td>
                    <td className="py-2">{intervalText(job.interval_seconds)}</td>
                    <td className="py-2">{fmtUnix(job.next_run_unix)}</td>
                    <td className="py-2">{fmtUnix(job.last_run_unix)}</td>
                    <td className="max-w-80 truncate py-2" title={job.last_result || job.last_error || ""}>
                      {job.last_error ? (
                        <span className="text-destructive">{job.last_error}</span>
                      ) : (
                        job.last_result || "—"
                      )}
                    </td>
                    <td className="py-2 text-right">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy !== "" || job.running}
                        onClick={() => void runSchedulerJob(job.name)}
                      >
                        {busy === "job:" + job.name ? "执行中…" : job.running ? "运行中" : "立即执行"}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p className="text-muted-foreground text-sm">调度器未启动或不可用。</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">目标侧清理清单</CardTitle>
          <CardDescription>
            {cleanup?.hint || "按 host 聚合的人工/AI 复核清单；实际删除由 session_exec 或运维执行。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-2">
            {Object.keys(hosts).length === 0 ? (
              <span className="text-muted-foreground text-sm">暂无需要清理的目标痕迹。</span>
            ) : (
              Object.entries(hosts).map(([host, n]) => (
                <Badge key={host} variant="secondary">
                  {host} · {n}
                </Badge>
              ))
            )}
          </div>
          {cleanupItems.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-muted-foreground text-left text-xs">
                  <tr>
                    <th className="py-2">主机</th>
                    <th className="py-2">通道</th>
                    <th className="py-2">类型</th>
                    <th className="py-2">清理命令</th>
                    <th className="py-2">备注</th>
                    <th className="py-2">滞留</th>
                  </tr>
                </thead>
                <tbody>
                  {cleanupItems.map((item) => (
                    <tr className="border-t" key={item.id}>
                      <td className="py-2">{item.host || "—"}</td>
                      <td className="py-2">{item.channel}</td>
                      <td className="py-2">{item.artifact_type || "—"}</td>
                      <td className="max-w-96 truncate py-2" title={item.cleanup_cmd}>
                        {item.cleanup_cmd || "—"}
                      </td>
                      <td className="max-w-72 truncate py-2" title={item.note}>
                        {item.note || "—"}
                      </td>
                      <td className="py-2">{item.age_days} 天</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
