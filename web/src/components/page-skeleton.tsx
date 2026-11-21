import { Skeleton } from "@/components/ui/skeleton";

// 页面级加载骨架：比一行「加载中…」更能表达结构，也避免布局跳动。
export function PageSkeleton({ rows = 4, dense = false }: { rows?: number; dense?: boolean }) {
  return (
    <div role="status" aria-label="加载中" className="flex flex-col gap-3 p-4">
      <div className="flex items-center justify-between gap-3">
        <Skeleton className={dense ? "h-6 w-36" : "h-8 w-48"} />
        <Skeleton className="h-8 w-24" />
      </div>
      {dense ? null : (
        <div className="grid gap-3 md:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      )}
      <div className="space-y-2">
        {Array.from({ length: rows }).map((_, i) => (
          <Skeleton key={i} className={dense ? "h-9 w-full" : "h-11 w-full"} />
        ))}
      </div>
      <span className="sr-only">加载中…</span>
    </div>
  );
}
