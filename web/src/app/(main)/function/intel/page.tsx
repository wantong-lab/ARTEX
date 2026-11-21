"use client";

import * as React from "react";

import { BookOpenIcon, ChevronLeftIcon, ChevronRightIcon, Loader2Icon, SearchIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { CVERecord, CVEStatus, KBEntry, KBEntryPage, KBInsights, KBSearchResult, SploitusItem } from "@/lib/types";

type TabKey = "kb" | "cve" | "sploitus";

const TABS: { key: TabKey; label: string }[] = [
  { key: "kb", label: "知识库" },
  { key: "cve", label: "CVE 情报" },
  { key: "sploitus", label: "Sploitus" },
];

const PAGE_SIZE = 10;

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export default function IntelPage() {
  const [tab, setTab] = React.useState<TabKey>("kb");
  const [error, setError] = React.useState("");

  return (
    <div className="flex flex-1 flex-col gap-4 p-4 md:p-6">
      <div className="flex items-center gap-2">
        <BookOpenIcon className="size-5" />
        <h1 className="font-semibold text-lg">情报</h1>
        <span className="text-muted-foreground text-xs">知识库 / CVE 语料 / Sploitus</span>
      </div>

      <div className="flex flex-wrap gap-1">
        {TABS.map((t) => (
          <Button key={t.key} size="sm" variant={tab === t.key ? "default" : "ghost"} onClick={() => setTab(t.key)}>
            {t.label}
          </Button>
        ))}
      </div>

      {error ? (
        <Card className="border-destructive/40 bg-destructive/5 p-3 text-destructive text-sm">{error}</Card>
      ) : null}

      {tab === "kb" ? <KnowledgeBlock onError={setError} /> : null}
      {tab === "cve" ? <CVEBlock onError={setError} /> : null}
      {tab === "sploitus" ? <SploitusBlock onError={setError} /> : null}
    </div>
  );
}

// ---------------- 知识库：检索 / 详情 / 入库 / 回填 ----------------

function KnowledgeBlock({ onError }: { onError: (msg: string) => void }) {
  const [q, setQ] = React.useState("");
  const [submitted, setSubmitted] = React.useState("");
  const [res, setRes] = React.useState<KBSearchResult | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [page, setPage] = React.useState(0);
  const [detail, setDetail] = React.useState<KBEntry | null>(null);
  const [detailLoading, setDetailLoading] = React.useState(false);
  const [addOpen, setAddOpen] = React.useState(false);
  const [addBusy, setAddBusy] = React.useState(false);
  const [notice, setNotice] = React.useState("");
  const [browse, setBrowse] = React.useState(false);
  const [pageData, setPageData] = React.useState<KBEntryPage | null>(null);
  const [browseOffset, setBrowseOffset] = React.useState(0);
  const [insights, setInsights] = React.useState<KBInsights | null>(null);
  const [editTarget, setEditTarget] = React.useState<KBEntry | null>(null);
  const BROWSE_LIMIT = 20;

  const entries = res?.entries ?? [];
  const totalPages = Math.max(1, Math.ceil(entries.length / PAGE_SIZE));
  const pageRows = entries.slice(page * PAGE_SIZE, page * PAGE_SIZE + PAGE_SIZE);

  const search = React.useCallback(async (query: string) => {
    if (!query.trim()) {
      setRes({ total: 0, entries: [] });
      setSubmitted("");
      return;
    }
    setLoading(true);
    onError("");
    try {
      const r = await api.kbSearch(query.trim(), 100);
      setRes(r);
      setSubmitted(query.trim());
      setPage(0);
    } catch (e) {
      onError(`知识库检索失败：${errText(e)}`);
      setRes({ total: 0, entries: [] });
    } finally {
      setLoading(false);
    }
  }, [onError]);

  const loadBrowse = React.useCallback(async (nextOffset: number) => {
    setLoading(true);
    onError("");
    try {
      const r = await api.kbEntries(nextOffset, BROWSE_LIMIT);
      setPageData(r);
      setBrowseOffset(nextOffset);
    } catch (e) {
      onError("知识库条目加载失败：" + errText(e));
    } finally {
      setLoading(false);
    }
  }, [onError]);

  const loadInsights = React.useCallback(async () => {
    try {
      setInsights(await api.kbInsights());
    } catch {
      setInsights(null);
    }
  }, []);

  React.useEffect(() => {
    if (!browse) return;
    void loadBrowse(0);
    void loadInsights();
  }, [browse, loadBrowse, loadInsights]);

  const submitEdit = async (form: { title: string; category: string; vuln_class: string; product: string; cve: string; content: string }) => {
    if (!editTarget) return;
    setAddBusy(true);
    onError("");
    try {
      await api.kbUpdate({ id: editTarget.id, ...form });
      setNotice("条目已更新 #" + editTarget.id);
      setEditTarget(null);
      await loadBrowse(browseOffset);
      await loadInsights();
    } catch (e) {
      onError("更新失败：" + errText(e));
    } finally {
      setAddBusy(false);
    }
  };

  const removeEntry = async (id: string) => {
    if (!window.confirm("删除条目 #" + id + "？（含 FTS 索引）")) return;
    onError("");
    try {
      await api.kbDelete(id);
      setNotice("已删除条目 #" + id);
      await loadBrowse(browseOffset);
      await loadInsights();
    } catch (e) {
      onError("删除失败：" + errText(e));
    }
  };

  const openDetail = async (id: string) => {
    setDetailLoading(true);
    try {
      setDetail(await api.kbEntry(id));
    } catch (e) {
      onError(`条目读取失败：${errText(e)}`);
    } finally {
      setDetailLoading(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <Card className="p-3">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void search(q);
          }}
        >
          <Input placeholder="检索知识库：组件 / 漏洞类 / 打法关键词" value={q} onChange={(e) => setQ(e.target.value)} />
          <Button type="submit" disabled={loading}>
            {loading ? <Loader2Icon className="size-4 animate-spin" /> : <SearchIcon className="size-4" />}
            检索
          </Button>
          <Button type="button" variant="outline" onClick={() => setAddOpen((v) => !v)}>
            入库
          </Button>
          <Button type="button" variant={browse ? "default" : "outline"} onClick={() => setBrowse((v) => !v)}>
            {browse ? "关闭浏览" : "全部条目"}
          </Button>
        </form>
        {notice ? <p className="mt-2 text-emerald-600 text-xs">{notice}</p> : null}
      </Card>

      {addOpen ? (
        <AddEntryCard
          busy={addBusy}
          onSubmit={async (form) => {
            setAddBusy(true);
            try {
              await api.kbAdd(form);
              setNotice("入库成功（重复 id 会幂等更新）");
              setAddOpen(false);
              if (submitted) void search(submitted);
            } catch (e) {
              onError(`入库失败：${errText(e)}`);
            } finally {
              setAddBusy(false);
            }
          }}
        />
      ) : null}

      {browse ? (
        <Card className="overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b p-3">
            <p className="font-medium text-sm">全部条目（服务端分页）</p>
            <span className="text-muted-foreground text-xs">
              {insights
                ? "共 " + insights.total + " 条 · ammo " + (insights.by_category.ammo ?? 0) + " · tactic " +
                  (insights.by_category.tactic ?? 0) + " · negative " + (insights.by_category.negative ?? 0)
                : "统计加载中…"}
            </span>
          </div>
          {pageData && pageData.entries.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-24">分档</TableHead>
                    <TableHead>标题</TableHead>
                    <TableHead className="w-28">漏洞类</TableHead>
                    <TableHead className="w-24">产品</TableHead>
                    <TableHead className="w-16">已验证</TableHead>
                    <TableHead className="w-36">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {pageData.entries.map((e) => (
                    <TableRow key={e.id}>
                      <TableCell>{e.category || "-"}</TableCell>
                      <TableCell className="max-w-lg truncate">
                        <button type="button" className="text-left hover:underline" onClick={() => void openDetail(e.id)}>
                          {e.title}
                        </button>
                      </TableCell>
                      <TableCell>{e.vuln_class || "-"}</TableCell>
                      <TableCell>{e.product || "-"}</TableCell>
                      <TableCell>{e.verified ? "是" : "否"}</TableCell>
                      <TableCell className="flex gap-1">
                        <Button size="sm" variant="ghost" onClick={() => setEditTarget(e)}>
                          编辑
                        </Button>
                        <Button size="sm" variant="destructive" onClick={() => void removeEntry(e.id)}>
                          删除
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <div className="flex items-center justify-end gap-2 border-t p-2 text-muted-foreground text-xs">
                <span>
                  共 {pageData.total} 条 · 第 {Math.floor(pageData.offset / BROWSE_LIMIT) + 1}/
                  {Math.max(1, Math.ceil(pageData.total / BROWSE_LIMIT))} 页
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={pageData.offset <= 0}
                  onClick={() => void loadBrowse(Math.max(0, pageData.offset - BROWSE_LIMIT))}
                >
                  上一页
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={pageData.offset + BROWSE_LIMIT >= pageData.total}
                  onClick={() => void loadBrowse(pageData.offset + BROWSE_LIMIT)}
                >
                  下一页
                </Button>
              </div>
            </>
          ) : (
            <p className="p-8 text-center text-muted-foreground text-sm">知识库暂无条目</p>
          )}
        </Card>
      ) : null}

      {editTarget ? (
        <AddEntryCard
          key={editTarget.id}
          busy={addBusy}
          submitLabel="保存修改"
          initial={{
            title: editTarget.title,
            category: editTarget.category || "tactic",
            vuln_class: editTarget.vuln_class || "",
            product: editTarget.product || "",
            cve: editTarget.cve || "",
            content: editTarget.content || editTarget.description || "",
          }}
          onSubmit={submitEdit}
        />
      ) : null}

      <Card className="overflow-hidden">
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-10 text-muted-foreground text-sm">
            <Loader2Icon className="size-4 animate-spin" /> 检索中…
          </div>
        ) : entries.length === 0 ? (
          <div className="p-10 text-center text-muted-foreground text-sm">
            {submitted ? "没有相关条目（严格匹配，宁缺毋滥）" : "输入关键词开始检索"}
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-24">分档</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead className="w-28">漏洞类</TableHead>
                  <TableHead className="w-24">产品</TableHead>
                  <TableHead className="w-16">已验证</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageRows.map((e) => (
                  <TableRow key={e.id} className="cursor-pointer" onClick={() => void openDetail(e.id)}>
                    <TableCell>{e.category || "-"}</TableCell>
                    <TableCell className="max-w-lg truncate">{e.title}</TableCell>
                    <TableCell>{e.vuln_class || "-"}</TableCell>
                    <TableCell>{e.product || "-"}</TableCell>
                    <TableCell>{e.verified ? "是" : "否"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <Pager page={page} totalPages={totalPages} onPage={setPage} total={entries.length} />
          </>
        )}
      </Card>

      <Dialog open={detail !== null || detailLoading} onOpenChange={(open) => !open && setDetail(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{detail?.title ?? "读取中…"}</DialogTitle>
            <DialogDescription>
              {detail ? `${detail.category} · ${detail.vuln_class} · id=${detail.id}` : " "}
            </DialogDescription>
          </DialogHeader>
          {detail ? (
            <pre className="max-h-[50vh] overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-3 text-xs">
              {detail.content || detail.description || "（无正文）"}
            </pre>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function AddEntryCard({
  busy,
  onSubmit,
  initial,
  submitLabel,
}: {
  busy: boolean;
  onSubmit: (form: { title: string; category: string; vuln_class: string; product: string; cve: string; content: string }) => Promise<void>;
  initial?: { title: string; category: string; vuln_class: string; product: string; cve: string; content: string };
  submitLabel?: string;
}) {
  const [form, setForm] = React.useState(
    initial ?? { title: "", category: "tactic", vuln_class: "", product: "", cve: "", content: "" },
  );
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));
  return (
    <Card className="flex flex-col gap-2 p-3">
      <p className="font-medium text-sm">{submitLabel ?? "手工入库"}</p>
      <div className="grid gap-2 md:grid-cols-2">
        <Input placeholder="标题（必填）" value={form.title} onChange={set("title")} />
        <Input placeholder="分档：ammo|tactic|negative" value={form.category} onChange={set("category")} />
        <Input placeholder="漏洞类：sqli|rce|..." value={form.vuln_class} onChange={set("vuln_class")} />
        <Input placeholder="产品（可选）" value={form.product} onChange={set("product")} />
        <Input placeholder="CVE（可选）" value={form.cve} onChange={set("cve")} />
      </div>
      <textarea
        className="min-h-24 rounded-md border bg-transparent p-2 text-sm"
        placeholder="正文：完整打法/步骤/负面结论（必填）"
        value={form.content}
        onChange={set("content")}
      />
      <div>
        <Button disabled={busy || !form.title.trim() || !form.content.trim()} onClick={() => void onSubmit(form)}>
          {busy ? <Loader2Icon className="size-4 animate-spin" /> : null} {submitLabel ?? "提交入库"}
        </Button>
      </div>
    </Card>
  );
}

function Pager({ page, totalPages, total, onPage }: { page: number; totalPages: number; total: number; onPage: (p: number) => void }) {
  return (
    <div className="flex items-center justify-end gap-2 border-t p-2 text-muted-foreground text-xs">
      <span>
        共 {total} 条 · 第 {page + 1}/{totalPages} 页
      </span>
      <Button size="sm" variant="ghost" disabled={page <= 0} onClick={() => onPage(page - 1)}>
        <ChevronLeftIcon className="size-4" />
      </Button>
      <Button size="sm" variant="ghost" disabled={page + 1 >= totalPages} onClick={() => onPage(page + 1)}>
        <ChevronRightIcon className="size-4" />
      </Button>
    </div>
  );
}

// ---------------- CVE 情报：检索 / 详情 / 同步进度 ----------------

function CVEBlock({ onError }: { onError: (msg: string) => void }) {
  const [q, setQ] = React.useState("");
  const [records, setRecords] = React.useState<CVERecord[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [searched, setSearched] = React.useState(false);
  const [status, setStatus] = React.useState<CVEStatus | null>(null);
  const [busy, setBusy] = React.useState(false);

  const refreshStatus = React.useCallback(async () => {
    try {
      setStatus(await api.cveStatus());
    } catch (e) {
      onError(`CVE 状态读取失败：${errText(e)}`);
    }
  }, [onError]);

  React.useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  // 同步进行中时轮询进度（2s）。
  React.useEffect(() => {
    if (!status?.running) return;
    const t = setInterval(() => void refreshStatus(), 2000);
    return () => clearInterval(t);
  }, [status?.running, refreshStatus]);

  const search = async () => {
    if (!q.trim()) {
      setRecords([]);
      setSearched(false);
      return;
    }
    setLoading(true);
    onError("");
    try {
      setRecords((await api.cveSearch(q.trim(), 50)).records ?? []);
      setSearched(true);
    } catch (e) {
      onError(`CVE 检索失败：${errText(e)}`);
    } finally {
      setLoading(false);
    }
  };

  const sync = status?.sync;
  const pct = sync && sync.total > 0 ? Math.min(100, Math.round((sync.start_index / sync.total) * 100)) : 0;

  return (
    <div className="flex flex-col gap-4">
      <Card className="p-3">
        <div className="flex items-center justify-between gap-2">
          <div className="text-sm">
            语料库：{status?.ok ? `${status.count ?? 0} 条（${status.year_min ?? "-"} - ${status.year_max ?? "-"}）` : "不可用"}
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={busy || status?.running}
              onClick={async () => {
                setBusy(true);
                try {
                  await api.cveSyncStart();
                  await refreshStatus();
                } catch (e) {
                  onError(`启动同步失败：${errText(e)}`);
                } finally {
                  setBusy(false);
                }
              }}
            >
              开始同步
            </Button>
            <Button
              size="sm"
              variant="destructive"
              disabled={!status?.running}
              onClick={async () => {
                if (!window.confirm("中断同步？已同步页会保留，下次从断点续跑。")) return;
                setBusy(true);
                try {
                  await api.cveSyncStop();
                  await refreshStatus();
                } catch (e) {
                  onError(`中断同步失败：${errText(e)}`);
                } finally {
                  setBusy(false);
                }
              }}
            >
              中断同步
            </Button>
          </div>
        </div>
        {sync ? (
          <div className="mt-2">
            <div className="h-2 w-full overflow-hidden rounded bg-muted">
              <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              {status?.running ? "同步中" : sync.done ? "已同步完成" : "未完成（可续跑）"} · {sync.start_index}/{sync.total} · 已入库{" "}
              {sync.processed}
            </p>
          </div>
        ) : null}
      </Card>

      <Card className="p-3">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void search();
          }}
        >
          <Input placeholder="检索 CVE：组件 / 产品 / CVE 编号" value={q} onChange={(e) => setQ(e.target.value)} />
          <Button type="submit" disabled={loading}>
            {loading ? <Loader2Icon className="size-4 animate-spin" /> : <SearchIcon className="size-4" />}
            检索
          </Button>
        </form>
      </Card>

      <Card className="overflow-hidden">
        {records.length === 0 ? (
          <div className="p-10 text-center text-muted-foreground text-sm">
            {searched ? "没有匹配的 CVE" : "输入关键词检索 CVE 语料库"}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-40">CVE</TableHead>
                <TableHead className="w-16">年份</TableHead>
                <TableHead>描述</TableHead>
                <TableHead className="w-20">CWE</TableHead>
                <TableHead className="w-24">EXP</TableHead>
                <TableHead className="w-28">性质</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="font-mono text-xs">{r.id}</TableCell>
                  <TableCell>{r.year}</TableCell>
                  <TableCell className="max-w-xl truncate text-xs">{r.description}</TableCell>
                  <TableCell className="text-xs">{r.cwe || "-"}</TableCell>
                  <TableCell>{r.has_exploit ? <Badge>有</Badge> : "-"}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">线索，非确证</Badge>
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

// ---------------- Sploitus ----------------

function SploitusBlock({ onError }: { onError: (msg: string) => void }) {
  const [q, setQ] = React.useState("");
  const [type, setType] = React.useState("");
  const [items, setItems] = React.useState<SploitusItem[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [searched, setSearched] = React.useState(false);

  const search = async () => {
    if (!q.trim()) return;
    setLoading(true);
    onError("");
    try {
      setItems((await api.cveSploitus(q.trim(), type, 30)).items ?? []);
      setSearched(true);
    } catch (e) {
      onError(`Sploitus 检索失败（可能无外网出口）：${errText(e)}`);
      setItems([]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <Card className="p-3">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void search();
          }}
        >
          <Input placeholder="检索 Sploitus：组件 / CVE / 漏洞名" value={q} onChange={(e) => setQ(e.target.value)} />
          <Input className="w-40" placeholder="type（可选）" value={type} onChange={(e) => setType(e.target.value)} />
          <Button type="submit" disabled={loading}>
            {loading ? <Loader2Icon className="size-4 animate-spin" /> : <SearchIcon className="size-4" />}
            检索
          </Button>
        </form>
        <p className="mt-1 text-muted-foreground text-xs">在线检索公开 EXP；结果仅供线索，不代表可直接利用。</p>
      </Card>

      <Card className="overflow-hidden">
        {items.length === 0 ? (
          <div className="p-10 text-center text-muted-foreground text-sm">
            {searched ? "没有结果" : "输入关键词检索 Sploitus"}
          </div>
        ) : (
          <ul className="divide-y">
            {items.map((it) => (
              <li key={it.id || it.href} className="flex items-center justify-between gap-3 p-3">
                <div className="min-w-0">
                  <a className="truncate font-medium text-sm hover:underline" href={it.href} target="_blank" rel="noreferrer">
                    {it.title}
                  </a>
                  <p className="text-muted-foreground text-xs">
                    {it.type} · {it.source || "-"} · {it.published || "-"}
                  </p>
                </div>
                {it.score ? <Badge variant="secondary">{it.score.toFixed(1)}</Badge> : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}