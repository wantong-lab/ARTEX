"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { ProxyPoolPanel } from "@/components/proxy-pool-panel";
import { api } from "@/lib/api";
import type { Settings } from "@/lib/types";

export default function ProxyPoolPage() {
  const [settings, setSettings] = React.useState<Settings | null>(null);
  React.useEffect(() => {
    api.settings().then(setSettings).catch(() => {});
  }, []);
  const [savingMode, setSavingMode] = React.useState(false);
  const mode = settings?.egress_mode || "direct";
  const effective = settings?.effective_proxy || "直连";
  const takeover = mode === "pool";
  const toggleTakeover = (on: boolean) => {
    setSavingMode(true);
    api.setSettings({ egress_mode: on ? "pool" : "direct" })
      .then(setSettings)
      .finally(() => setSavingMode(false));
  };
  return (
    <div className="flex flex-col gap-4 px-4 lg:px-6">
      <header className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-end">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight">出口 IP 代理池</h1>
          <p className="text-muted-foreground text-sm">
            管理出口代理条目、探测/切换活动代理与兜底。仅当右上角「接管出口」开启（egress_mode=pool）时订阅/代理池才真正生效；直连/固定代理模式不经过本页出口。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs xl:justify-end">
          <label className="flex items-center gap-2 rounded-full border px-2.5 py-1 text-muted-foreground">
            接管出口
            <Switch checked={takeover} disabled={!settings || savingMode} onCheckedChange={toggleTakeover} />
          </label>
          <Badge variant={mode === "pool" ? "default" : "secondary"}>出口模式 {mode}</Badge>
          <span className="inline-flex max-w-[360px] items-center gap-1 rounded-full border px-2.5 py-1 text-muted-foreground">
            当前生效
            <span className="max-w-[220px] truncate font-mono text-foreground" title={effective}>{effective}</span>
          </span>
          <a className="text-muted-foreground underline underline-offset-4" href="/system/settings">系统配置</a>
        </div>
      </header>
      <ProxyPoolPanel />
    </div>
  );
}
