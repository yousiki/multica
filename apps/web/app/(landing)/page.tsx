"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useNavigationStore } from "@/features/navigation";
import { MulticaLanding } from "@/features/landing/components/multica-landing";
import { MulticaIcon } from "@/components/multica-icon";

export default function LandingPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && user) {
      const lastPath = useNavigationStore.getState().lastPath;
      router.replace(lastPath);
    }
  }, [isLoading, user, router]);

  if (isLoading || user) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6" />
      </div>
    );
  }

  return <MulticaLanding />;
}
