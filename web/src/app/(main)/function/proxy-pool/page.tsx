"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { ProxyPoolPanel } from "@/components/proxy-pool-panel";
import { api } from "@/lib/api";
import type { Settings } from "@/lib/types";

export default function ProxyPoolPage() {
  const [settings, setSettings] = React.useState<Settings | null>(null);
  React.useEffect(() => {
    api.settings().then(setSettings).catch(() => {});
  }, []);
  const mode = settings?.egress_mode || "direct";
  const effective = settings?.effective_proxy || "直连";
  return (
    <div className="flex flex-col gap-4 px-4 lg:px-6">
      <header className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">出口 IP 代理池</h1>
          <p className="text-muted-foreground text-sm">
            管理出口代理条目、探测/切换活动代理与兜底；是否接管平台出口由「系统配置 → 出口模式」决定。
          </p>
        </div>
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2 text-xs">
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
