import { PageSkeleton } from "@/components/page-skeleton";

export default function Loading() {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-6">
      <div className="w-full max-w-5xl">
        <PageSkeleton rows={5} />
      </div>
    </div>
  );
}
