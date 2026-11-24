"use client";

import * as React from "react";
import { ChevronDownIcon, RefreshCwIcon } from "lucide-react";

import { Switch } from "@/components/ui/switch";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { PageSkeleton } from "@/components/page-skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { MihomoImportResult, ProxyPoolState, ProxySubscriptionNode, ProxySubscriptionStatus } from "@/lib/types";

// ---------------- 代理池（M4-03）：录入 / 探测 / 切换 / 回退 ----------------

export function ProxyPoolPanel() {
  const [state, setState] = React.useState<ProxyPoolState | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [form, setForm] = React.useState({ name: "", url: "" });
  const [globalURL, setGlobalURL] = React.useState("");
  const [egress, setEgress] = React.useState("");
  const [mihomoYAML, setMihomoYAML] = React.useState("");
  const [subscriptionURL, setSubscriptionURL] = React.useState("");
  const [subStatus, setSubStatus] = React.useState<ProxySubscriptionStatus | null>(null);
  const [subBusy, setSubBusy] = React.useState(false);
  const [nodes, setNodes] = React.useState<ProxySubscriptionNode[]>([]);
  const [expandedNodeRow, setExpandedNodeRow] = React.useState<number | null>(null);
  const [autoRotate, setAutoRotate] = React.useState(false);
  const [selectionMode, setSelectionMode] = React.useState("auto");
  const [nodeBusy, setNodeBusy] = React.useState("");
  const [delays, setDelays] = React.useState<Record<string, number>>({});
  const [shareLinks, setShareLinks] = React.useState("");
  const [gatewayURL, setGatewayURL] = React.useState("");
  const [gatewayCount, setGatewayCount] = React.useState("5");
  const [sourceBusy, setSourceBusy] = React.useState(false);
  const [mihomoPrefix, setMihomoPrefix] = React.useState("mh-");
  const [importing, setImporting] = React.useState(false);
  const [importResult, setImportResult] = React.useState<MihomoImportResult | null>(null);

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      const [pool, sub, nodeState] = await Promise.all([
        api.proxyPool(),
        api.proxyPoolSubscription().catch(() => null),
        api.proxyPoolSubscriptionNodes().catch(() => null),
      ]);
      setState(pool);
      setSubStatus(sub);
      setNodes(nodeState?.nodes ?? []);
      setSelectionMode(nodeState?.selection_mode || sub?.selection_mode || "auto");
      setAutoRotate(!!(sub?.auto_rotate ?? (nodeState?.selection_mode === "auto")));
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

  const applyImportResult = (r: MihomoImportResult) => {
    setImportResult(r);
    setNotice(
      "导入完成：新增 " + r.imported_count + " 条，重复 " + r.duplicates +
      " 条，跳过 " + r.skipped.length + " 条（ss/vmess 等需 mihomo 内核转发）",
    );
  };

  const startSubscriptionProxy = () => {
    setSubBusy(true);
    setError("");
    api.proxyPoolSubscriptionStart(subscriptionURL.trim())
      .then(async (st) => {
        setSubStatus(st);
        setNotice(st.local_proxy ? "订阅代理已启动：" + st.local_proxy : "订阅代理已启动");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSubBusy(false));
  };

  const refreshSubscriptionProxy = () => {
    setSubBusy(true);
    setError("");
    api.proxyPoolSubscriptionRefresh()
      .then(async (st) => {
        setSubStatus(st);
        setNotice("订阅已刷新");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSubBusy(false));
  };

  const stopSubscriptionProxy = () => {
    setSubBusy(true);
    setError("");
    api.proxyPoolSubscriptionStop()
      .then(async (st) => {
        setSubStatus(st);
        setNotice("订阅代理已停止");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSubBusy(false));
  };

  const selectSubscriptionNode = (mode: "auto" | "manual", name?: string) => {
    setNodeBusy(name || mode);
    setError("");
    api.proxyPoolSubscriptionSelect({ mode, name })
      .then(async () => {
        setNotice(mode === "auto" ? "已切换为自动选择" : "已切换到节点：" + (name || ""));
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setNodeBusy(""));
  };

  const probeSubscriptionNode = (name: string) => {
    setNodeBusy("probe:" + name);
    setError("");
    api.proxyPoolSubscriptionProbe(name)
      .then((r) => setDelays((prev) => ({ ...prev, [name]: r.delay })))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setNodeBusy(""));
  };

  const toggleAutoRotate = (on: boolean) => {
    setNodeBusy("auto-rotate");
    setError("");
    api.proxyPoolSubscriptionAutoRotate(on)
      .then(async () => {
        setAutoRotate(on);
        setNotice(on ? "封禁自动换 IP 已开启" : "封禁自动换 IP 已关闭");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setNodeBusy(""));
  };

  const importShareLinks = () => {
    setSourceBusy(true);
    setError("");
    api.proxyPoolNodesImport(shareLinks.trim())
      .then(async () => {
        setNotice("分享链接节点已导入并启动");
        setShareLinks("");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSourceBusy(false));
  };

  const startGateway = () => {
    setSourceBusy(true);
    setError("");
    api.proxyPoolGatewayStart(gatewayURL.trim(), Number(gatewayCount) || 5)
      .then(async () => {
        setNotice("住宅网关已启动");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSourceBusy(false));
  };

  const rotateGateway = () => {
    setSourceBusy(true);
    setError("");
    api.proxyPoolGatewayRotate()
      .then(async () => {
        setNotice("已轮换住宅网关 SID");
        await refresh();
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSourceBusy(false));
  };

  return (
    <div className="flex flex-col gap-4">
      {error ? <Card className="border-destructive/40 bg-destructive/5 p-3 text-destructive text-sm">{error}</Card> : null}
      {notice ? <Card className="border-emerald-500/40 bg-emerald-500/5 p-3 text-emerald-600 text-sm">{notice}</Card> : null}

      <Card className="flex flex-col gap-3 border-primary/30 bg-muted/10 p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="font-semibold text-sm">Clash 订阅代理（mihomo 内核，支持 vless/hysteria2）</p>
          <Badge variant={subStatus?.running ? "default" : "secondary"}>
            {subStatus?.observed_state || (subStatus?.running ? "ready" : "disabled")}
          </Badge>
        </div>
        <p className="text-muted-foreground text-xs">
          {subStatus?.mihomo_available
            ? "mihomo：" + (subStatus.mihomo_binary || "可用")
            : "mihomo 不可用：" + (subStatus?.mihomo_error || "未检测到，请配置 ARTEX_MIHOMO_PATH")}
        </p>
        {subStatus?.running ? (
          <p className="text-xs">
            本地代理：<code>{subStatus.local_proxy}</code> · 条目 #{subStatus.entry_id} · 已设为活动代理
          </p>
        ) : null}
        {subStatus?.last_error ? <p className="text-destructive text-xs">{subStatus.last_error}</p> : null}
        {subStatus?.last_fetch_at ? <p className="text-muted-foreground text-xs">最近刷新：{subStatus.last_fetch_at}</p> : null}
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="min-w-64 flex-1"
            placeholder="Clash 订阅 URL（支持 base64/vless 订阅，自动尝试 meta 格式）"
            value={subscriptionURL}
            onChange={(e) => setSubscriptionURL(e.target.value)}
          />
          {subStatus?.running ? (
            <Button size="sm" variant="outline" disabled={subBusy} onClick={refreshSubscriptionProxy}>
              {subBusy ? "刷新中…" : "刷新订阅"}
            </Button>
          ) : null}
          {subStatus?.running ? (
            <Button size="sm" variant="destructive" disabled={subBusy} onClick={stopSubscriptionProxy}>
              {subBusy ? "停止中…" : "停止订阅代理"}
            </Button>
          ) : (
            <Button size="sm" disabled={subBusy || !subscriptionURL.trim() || !subStatus?.mihomo_available} onClick={startSubscriptionProxy}>
              {subBusy ? "启动中…" : "启动订阅代理"}
            </Button>
          )}
        </div>
        <p className="text-muted-foreground text-xs">
          启动后自动登记 127.0.0.1 本地代理并设为活动代理；到「系统配置 → 出口代理」把出口模式切到「代理池接管」。
        </p>
      </Card>

      <Card className="flex flex-col gap-2 p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="font-medium text-sm">分享链接节点（vmess/ss/trojan/vless）</p>
          <span className="text-muted-foreground text-xs">
            当前来源：{subStatus?.source_kind || "无"}；导入会替换当前 active 来源
          </span>
        </div>
        <textarea
          className="min-h-20 rounded-md border bg-transparent p-2 font-mono text-xs"
          placeholder="每行一条 vmess:// / ss:// / trojan:// / vless://，或整段 base64 订阅文本"
          value={shareLinks}
          onChange={(e) => setShareLinks(e.target.value)}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" disabled={sourceBusy || !shareLinks.trim()} onClick={importShareLinks}>
            {sourceBusy ? "导入中…" : "导入并启动"}
          </Button>
          <span className="text-muted-foreground text-xs">解析后交给 mihomo，节点可在代理池行内展开选择/测延迟。</span>
        </div>
      </Card>

      <Card className="flex flex-col gap-2 p-3">
        <p className="font-medium text-sm">住宅网关（socks5/http/https）</p>
        <div className="flex flex-wrap gap-2">
          <Input
            className="min-w-64 flex-1"
            placeholder="socks5://user-region-XX-sid-rand@host:port"
            value={gatewayURL}
            onChange={(e) => setGatewayURL(e.target.value)}
          />
          <Input className="w-24" placeholder="节点数" value={gatewayCount} onChange={(e) => setGatewayCount(e.target.value)} />
          <Button size="sm" disabled={sourceBusy || !gatewayURL.trim()} onClick={startGateway}>
            {sourceBusy ? "处理中…" : "启动网关"}
          </Button>
          <Button size="sm" variant="outline" disabled={sourceBusy || subStatus?.source_kind !== "gateway"} onClick={rotateGateway}>
            轮换 SID
          </Button>
        </div>
        <p className="text-muted-foreground text-xs">
          网关用户名中的 region/sid 段会生成多条住宅出口；密码加密落库，不写日志。
        </p>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex flex-col gap-2 p-3">
          <p className="font-medium text-sm">录入代理（http/https/socks5，支持认证）</p>
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]">
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
          <p className="font-medium text-sm">代理池兜底代理（pool 模式第二级：活动代理失败时回退）</p>
          <div className="flex flex-wrap gap-2">
            <Input className="min-w-48 flex-1" placeholder="留空 = 直连" value={globalURL} onChange={(e) => setGlobalURL(e.target.value)} />
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

      <Card className="p-0">
        <details className="group">
          <summary className="flex cursor-pointer items-center justify-between gap-2 p-4 text-sm font-medium">
            <span>高级：直接导入 http/socks5 节点（YAML）</span>
            <span className="text-muted-foreground text-xs group-open:hidden">展开</span>
            <span className="text-muted-foreground hidden text-xs group-open:inline">收起</span>
          </summary>
          <div className="flex flex-col gap-2 border-t p-4">
        <p className="text-muted-foreground text-xs">
          仅用于直接可用的 http/socks5 节点；Clash/vless 订阅请用上方 mihomo 订阅代理。
        </p>
        <textarea
          className="min-h-28 rounded-md border bg-transparent p-2 font-mono text-xs"
          placeholder="或粘贴 mihomo config.yaml / 订阅内容（proxies 列表）"
          value={mihomoYAML}
          onChange={(e) => setMihomoYAML(e.target.value)}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Input className="w-32" placeholder="名称前缀" value={mihomoPrefix} onChange={(e) => setMihomoPrefix(e.target.value)} />
          <Button
            size="sm"
            variant="outline"
            disabled={busy || importing || !mihomoYAML.trim()}
            onClick={() => {
              setImporting(true);
              void run(async () => {
                try {
                  const r = await api.proxyPoolImportMihomo({ yaml: mihomoYAML, name_prefix: mihomoPrefix });
                  applyImportResult(r);
                  setMihomoYAML("");
                  await refresh();
                } finally {
                  setImporting(false);
                }
              });
            }}
          >
            {importing ? "导入中…" : "导入粘贴内容"}
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
          </div>
        </details>
      </Card>

      <Card className="overflow-hidden">
        <div className="flex items-center justify-between p-2">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <p className="font-medium text-sm">代理池（{state?.entries.length ?? 0}）</p>
            <p className="text-muted-foreground text-xs">当前是否接管全局出口由「系统配置 → 出口模式」决定；本页只管理条目/活动代理。</p>
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
          <PageSkeleton rows={4} dense />
        ) : (state?.entries.length ?? 0) === 0 ? (
          <div className="p-10 text-center text-muted-foreground text-sm">暂无代理，先启动 Clash 订阅或录入一条手工代理。</div>
        ) : (
          <div className="overflow-x-auto">
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
                <React.Fragment key={e.id}>
                <TableRow>
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
                    {e.name === "clash-sub" ? (
                      <Button size="sm" variant="ghost" onClick={() => setExpandedNodeRow(expandedNodeRow === e.id ? null : e.id)}>
                        {expandedNodeRow === e.id ? "收起节点" : "节点"}
                      </Button>
                    ) : null}
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
                {expandedNodeRow === e.id ? (
                  <TableRow>
                    <TableCell colSpan={9} className="bg-muted/20 p-3">
                      <div className="flex flex-col gap-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <Button
                            size="sm"
                            variant={selectionMode === "auto" ? "default" : "outline"}
                            disabled={!!nodeBusy || !subStatus?.running}
                            onClick={() => selectSubscriptionNode("auto")}
                          >
                            自动选择
                          </Button>
                          <label className="flex items-center gap-2 text-muted-foreground text-xs">
                            封禁自动换 IP
                            <Switch checked={autoRotate} disabled={!!nodeBusy || !subStatus?.running} onCheckedChange={toggleAutoRotate} />
                          </label>
                          <span className="text-muted-foreground text-xs">
                            当前：{selectionMode === "auto" ? "自动" : subStatus?.selected_node || "-"}
                          </span>
                        </div>
                        {nodes.length === 0 ? (
                          <p className="rounded border border-dashed p-3 text-center text-muted-foreground text-xs">
                            启动订阅后显示节点
                          </p>
                        ) : (
                          <div className="grid max-h-80 gap-2 overflow-y-auto pr-1 sm:grid-cols-2 xl:grid-cols-4">
                            {nodes.map((n) => {
                              const selected = selectionMode === "manual" && subStatus?.selected_node === n.name;
                              const probing = nodeBusy === "probe:" + n.name;
                              return (
                                <div key={n.name} className={"rounded-md border p-2 text-xs " + (selected ? "border-primary bg-primary/5" : "")}>
                                  <div className="flex items-center justify-between gap-2">
                                    <span className="truncate font-medium" title={n.name}>{n.name}</span>
                                    <Badge variant="secondary">{n.type}</Badge>
                                  </div>
                                  <div className="mt-1 flex items-center justify-between gap-2 text-muted-foreground">
                                    <span className="truncate" title={n.server}>{n.server}:{n.port}</span>
                                    <span>{delays[n.name] ? delays[n.name] + "ms" : "-"}</span>
                                  </div>
                                  <div className="mt-2 flex gap-1">
                                    <Button size="sm" variant="ghost" disabled={!!nodeBusy || !subStatus?.running} onClick={() => probeSubscriptionNode(n.name)}>
                                      {probing ? "测速中…" : "测延迟"}
                                    </Button>
                                    <Button size="sm" variant={selected ? "secondary" : "outline"} disabled={!!nodeBusy || !subStatus?.running || selected} onClick={() => selectSubscriptionNode("manual", n.name)}>
                                      {selected ? "使用中" : "使用"}
                                    </Button>
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ) : null}
                </React.Fragment>
              ))}
            </TableBody>
          </Table>
          </div>
        )}
      </Card>
    </div>
  );
}
