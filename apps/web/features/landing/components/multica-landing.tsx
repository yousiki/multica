import Image from "next/image";
import Link from "next/link";
import { MulticaIcon } from "@/components/multica-icon";
import { cn } from "@/lib/utils";

const githubUrl = "https://github.com/multica-ai/multica";

export function MulticaLanding() {
  return (
    <div className="relative min-h-full overflow-hidden bg-[#05070b] text-white">
      <LandingBackdrop />

      <header className="absolute inset-x-0 top-0 z-30 bg-transparent">
        <div className="mx-auto flex h-[76px] max-w-[1320px] items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link href="#product" className="flex items-center gap-3">
            <MulticaIcon className="size-5 text-white" noSpin />
            <span className="text-[18px] font-semibold tracking-[0.04em] text-white/92 lowercase sm:text-[20px]">
              multica
            </span>
          </Link>

          <div className="flex items-center gap-2.5 sm:gap-3">
            <Link
              href={githubUrl}
              target="_blank"
              rel="noreferrer"
              className={headerButtonClassName("ghost")}
            >
              <GitHubMark className="size-3.5" />
              GitHub
            </Link>
            <Link href="/login" className={headerButtonClassName("solid")}>
              Log in
            </Link>
          </div>
        </div>
      </header>

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="font-[family-name:var(--font-serif)] text-[3.65rem] leading-[0.93] tracking-[-0.038em] text-white drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)] sm:text-[4.85rem] lg:text-[6.4rem]">
              Your next 10 hires
              <br />
              won&apos;t be human.
            </h1>

            <p className="mx-auto mt-7 max-w-[820px] text-[15px] leading-7 text-white/84 sm:text-[17px]">
              Multica is project management for human + agent teams. Assign
              tasks, manage runtimes, compound skills, all in one place.
            </p>

            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link href="/login" className={heroButtonClassName("solid")}>
                Start free trial
              </Link>
              <Link
                href={githubUrl}
                target="_blank"
                rel="noreferrer"
                className={heroButtonClassName("ghost")}
              >
                <GitHubMark className="size-4" />
                GitHub
              </Link>
            </div>

            <div id="preview" className="mt-14 sm:mt-16">
              <ProductImagePlaceholder />
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}

function LandingBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0">
      <Image
        src="/images/landing-bg.jpg"
        alt=""
        fill
        priority
        className="object-cover object-center"
      />
      <div className="absolute inset-0 bg-[linear-gradient(180deg,rgba(3,5,8,0)_0%,rgba(3,5,8,0)_42%,rgba(3,5,8,0.58)_100%)]" />
    </div>
  );
}

function GitHubMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      className={className}
      fill="currentColor"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2 .37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.65 7.65 0 0 1 8 4.84c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

function ProductImagePlaceholder() {
  return (
    <div className="mx-auto max-w-[1180px]">
      <div className="relative overflow-hidden rounded-[24px] border border-dashed border-white/14 bg-[linear-gradient(135deg,rgba(255,255,255,0.08)_0%,rgba(255,255,255,0.03)_100%)] shadow-[0_38px_120px_-42px_rgba(0,0,0,0.9)]">
        <div className="aspect-[16/9] w-full" />

        <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,rgba(125,211,252,0.16),transparent_28%),radial-gradient(circle_at_72%_64%,rgba(167,139,250,0.14),transparent_30%)]" />
        <div className="absolute inset-0 opacity-20 [background-image:linear-gradient(rgba(255,255,255,0.08)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.08)_1px,transparent_1px)] [background-size:32px_32px]" />

        <div className="absolute inset-0 flex flex-col items-center justify-center px-6 text-center">
          <div className="grid size-18 place-items-center rounded-[22px] border border-white/12 bg-black/22 shadow-[0_18px_40px_-24px_rgba(0,0,0,0.75)]">
            <svg
              viewBox="0 0 24 24"
              aria-hidden="true"
              className="size-8 text-white/82"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <rect x="3.5" y="5" width="17" height="14" rx="2.5" />
              <circle cx="9" cy="10" r="1.6" />
              <path d="m20.5 16-4.8-4.8a1 1 0 0 0-1.4 0L8 17.5" />
              <path d="m11.5 14.5 1.8-1.8a1 1 0 0 1 1.4 0l2.8 2.8" />
            </svg>
          </div>

          <h2 className="mt-6 text-[1.8rem] font-semibold tracking-[-0.04em] text-white sm:text-[2.2rem]">
            Real product screenshot goes here
          </h2>

          <p className="mt-3 max-w-[520px] text-[14px] leading-7 text-white/68 sm:text-[15px]">
            Keeping the space and visual weight of the hero preview, while
            leaving the center as a clean image placeholder for now.
          </p>
        </div>
      </div>
    </div>
  );
}

function headerButtonClassName(tone: "ghost" | "solid") {
  return cn(
    "inline-flex items-center justify-center gap-2 rounded-[11px] px-4 py-2.5 text-[13px] font-semibold transition-colors",
    tone === "solid"
      ? "bg-white text-[#0a0d12] hover:bg-white/92"
      : "border border-white/18 bg-black/16 text-white backdrop-blur-sm hover:bg-black/24",
  );
}

function heroButtonClassName(tone: "ghost" | "solid") {
  return cn(
    "inline-flex items-center justify-center gap-2 rounded-[12px] px-5 py-3 text-[14px] font-semibold transition-colors",
    tone === "solid"
      ? "bg-white text-[#0a0d12] hover:bg-white/92"
      : "border border-white/18 bg-black/16 text-white backdrop-blur-sm hover:bg-black/24",
  );
}
