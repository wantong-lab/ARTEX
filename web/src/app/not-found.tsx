import Link from "next/link";

// 自定义 404：静态导出时生成 404.html，Go 静态服务对未知路径直接以 404 返回本页。
export default function NotFound() {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center gap-4 p-8 text-center">
      <p className="text-muted-foreground text-sm font-medium tracking-widest">404</p>
      <h1 className="text-2xl font-semibold tracking-tight">页面不存在</h1>
      <p className="text-muted-foreground text-sm max-w-md">
        你访问的地址不存在或已被移动，请检查链接后重试。
      </p>
      <Link
        href="/function/tasks"
        className="bg-primary text-primary-foreground hover:bg-primary/90 inline-flex h-9 items-center rounded-lg px-4 text-sm font-medium transition-colors"
      >
        返回任务页
      </Link>
    </div>
  );
}
