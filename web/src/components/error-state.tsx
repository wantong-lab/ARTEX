"use client";

import { AlertTriangle, Home, RefreshCw } from "lucide-react";
import Link from "next/link";

import { Button } from "@/components/ui/button";

export function ErrorState({
  title = "页面出错了",
  description = "该页面渲染时遇到异常，可以重试或返回仪表盘。",
  error,
  onRetry,
}: {
  title?: string;
  description?: string;
  error?: Error & { digest?: string };
  onRetry?: () => void;
}) {
  return (
    <div role="alert" className="flex min-h-[60vh] flex-col items-center justify-center gap-4 p-6 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
        <AlertTriangle className="size-6" />
      </div>
      <div className="space-y-1">
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="max-w-md text-muted-foreground text-sm">{description}</p>
      </div>
      <div className="flex flex-wrap items-center justify-center gap-2">
        {onRetry ? (
          <Button onClick={onRetry}>
            <RefreshCw className="size-4" /> 重试
          </Button>
        ) : null}
        <Button variant="outline" asChild>
          <Link href="/dashboard">
            <Home className="size-4" /> 返回仪表盘
          </Link>
        </Button>
      </div>
      {error?.message ? (
        <details className="w-full max-w-md text-left text-muted-foreground text-xs">
          <summary className="cursor-pointer select-none">错误详情</summary>
          <pre className="mt-2 overflow-x-auto whitespace-pre-wrap rounded-md bg-muted p-3">
            {error.message}
            {error.digest ? " (digest: " + error.digest + ")" : ""}
          </pre>
        </details>
      ) : null}
    </div>
  );
}
