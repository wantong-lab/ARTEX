"use client";

import { useEffect } from "react";

import { toast } from "sonner";

// 把 http() 识别到的网络/5xx 错误统一提示出来：页面各自的 inline 错误仍保留，
// 但导航、后台轮询等没有局部错误区的请求也不会静默失败。
export function ApiErrorToaster() {
  useEffect(() => {
    function onError(e: Event) {
      const detail = (e as CustomEvent<{ status?: number; message?: string }>).detail;
      if (!detail?.message) return;
      toast.error(detail.message, { id: "api-error-" + (detail.status ?? "network") });
    }
    window.addEventListener("artex:api-error", onError);
    return () => window.removeEventListener("artex:api-error", onError);
  }, []);
  return null;
}
