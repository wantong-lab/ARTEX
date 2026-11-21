"use client";

import { useEffect } from "react";

import { ErrorState } from "@/components/error-state";

export default function MainError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error("[ui] main page error", error);
  }, [error]);
  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <ErrorState error={error} onRetry={reset} />
    </div>
  );
}
