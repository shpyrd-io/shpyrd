"use client";

import { Calculator, Check, ClipboardCheck, Gauge, LockKeyhole, RotateCcw, Wallet } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { filmEnter, filmInset, filmPiece, useFilm, useTyped } from "@/components/film";

// The films of the home's "Not the one who built it?" cards (Giovani,
// 2026-10-20), in place of photographs, which the site uses nowhere else.
// Each tells what its page tells (content/site/solutions/developers.mdx,
// internal-apps.mdx, implementation-partners.mdx), in the glass pieces and
// orange marks of the feature cards above.

// A line under a film saying what is happening, each rising in as the one
// before rises away, as the visitors in the sleeping app's film.
function Caption({ lines, at }: { lines: readonly string[]; at: number }) {
  return (
    <div className="relative h-5 w-full shrink-0 overflow-hidden text-center text-[10.5px] text-muted-foreground">
      {lines.map((line, i) => (
        <span
          key={line}
          className={cn(
            "absolute inset-x-0 top-0.5 transition-[translate,opacity] duration-700 ease-out",
            i === at ? "translate-y-0 opacity-100" : i < at ? "-translate-y-[150%] opacity-0" : "translate-y-[150%] opacity-0",
          )}
        >
          {line}
        </span>
      ))}
    </div>
  );
}

const cursor = <span className="ml-px inline-block h-[1em] w-px translate-y-[0.15em] animate-pulse bg-current" />;

// --- Developers: "The first deployment, the next change, the release
// history." Deployed from local code, then a change from Git; the logs show
// it failing; rolled back to v1, and the logs are fine again. Typed below
// as it would be at the command line, or by an agent.

const DEV = [
  ["v1", 2000],
  ["v2", 2000],
  ["errors", 2200],
  ["rollback", 2000],
  ["back", 2800],
] as const;
type DevMoment = (typeof DEV)[number][0];

const commands: Record<DevMoment, string> = {
  v1: "shpyrd deploy",
  v2: "git push",
  errors: "shpyrd logs",
  rollback: "shpyrd rollback v1",
  back: "shpyrd rollback v1",
};

export function DevelopersFilm() {
  const { root, name, past, round, still } = useFilm(DEV);
  const at = (m: DevMoment) => still || past(m);
  const typed = useTyped(commands[name], !still, 65);
  const back = at("back");

  const releases = [
    ...(at("v2") ? [{ v: "v2", note: back ? "rolled back" : "next change", live: !back, from: "Git" }] : []),
    { v: "v1", note: "first deployment", live: !at("v2") || back, from: "local code" },
  ];
  const logs = back
    ? [{ line: "GET /checkout 200 · 120 ms", bad: false }]
    : at("errors")
      ? [
          { line: "GET /checkout 500 · 2.4 s", bad: true },
          { line: "GET /checkout 500 · 2.6 s", bad: true },
        ]
      : [];

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className="flex size-full flex-col gap-2">
        {/* The project: the app, what runs in it, its releases. */}
        <div className={cn(filmPiece, "grid gap-2 px-3 py-2.5")}>
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium text-foreground">yourapp.com</span>
            <span className="truncate text-[10px] text-muted-foreground">web · worker · database</span>
          </div>
          <div className="grid gap-1">
            {releases.map((r) => (
              <span key={r.v} className={cn("flex items-center gap-2 text-[10.5px]", filmEnter)}>
                <span className={cn("w-5 font-semibold", r.live ? "text-primary" : "text-muted-foreground")}>{r.v}</span>
                <span className={cn("flex-1 truncate", r.live ? "text-foreground" : "text-muted-foreground")}>
                  <span className={cn(r.note === "rolled back" && "line-through")}>{r.note}</span>
                  <span className="text-muted-foreground"> · {r.from}</span>
                </span>
                {r.live && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-success/12 px-1.5 text-[9.5px] font-medium text-success">
                    <span className="size-1.5 rounded-full bg-success" />
                    live
                  </span>
                )}
              </span>
            ))}
          </div>
        </div>

        {/* The logs. */}
        <div className="grid min-h-10 flex-1 content-start gap-0.5 rounded-2xl bg-foreground/[0.04] px-3 py-2 text-[10px]">
          <span className="text-muted-foreground">Logs</span>
          {logs.map((l) => (
            <span key={l.line} className={cn(l.bad ? "text-destructive" : "text-success", filmEnter)}>
              {l.line}
            </span>
          ))}
        </div>

        {/* What is typed. */}
        <div className="flex items-center gap-1.5 rounded-2xl bg-foreground/[0.82] px-3 py-1.5 text-[10.5px] text-background">
          <span className="text-primary">$</span>
          <span>
            {still ? commands.back : typed}
            {!still && cursor}
          </span>
          {name === "rollback" && <RotateCcw className="ml-auto size-3 animate-spin text-primary [animation-direction:reverse]" />}
        </div>
      </div>
    </div>
  );
}

// --- Internal apps: "An approval tool, in use by the right team." Ana
// signs in with the company login; the portal shows her the apps she may
// use, Payroll locked, not hers; a new release of Approvals reaches everyone.

const IT = [
  ["login", 2400],
  ["portal", 2000],
  ["locked", 2200],
  ["update", 3000],
] as const;

const tiles = [
  { icon: ClipboardCheck, name: "Approvals", locked: false },
  { icon: Gauge, name: "Ops dashboard", locked: false },
  { icon: Calculator, name: "Quotes", locked: false },
  { icon: Wallet, name: "Payroll", locked: true },
] as const;

const itCaption = [
  "Ana signs in with the company login",
  "The portal shows the apps she may use",
  "Payroll is not for her team: locked",
  "A new release reaches everyone",
];

export function TeamsFilm() {
  const { root, name, past, round, still } = useFilm(IT);
  const at = (m: (typeof IT)[number][0]) => still || past(m);
  const email = useTyped("ana@yourapp.com", name === "login" && !still, 70);
  const portal = at("portal");

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className="flex size-full flex-col gap-2">
        <div className="relative flex-1">
          {!portal ? (
            // The company login.
            <div className={cn(filmPiece, "absolute inset-x-3 top-1/2 grid -translate-y-1/2 gap-2 px-4 py-3.5")}>
              <span className="text-center font-medium text-foreground">Sign in to your company</span>
              <span className="rounded-md bg-foreground/[0.04] px-2.5 py-1.5 text-[10.5px] text-foreground ring-1 ring-foreground/8">
                {still ? "ana@yourapp.com" : email}
                {!still && cursor}
              </span>
              <span className="rounded-md bg-primary py-1.5 text-center text-[10.5px] font-medium text-white">Continue with company login</span>
            </div>
          ) : (
            // The portal: the apps she may use.
            <div className={cn(filmPiece, "absolute inset-0 grid grid-rows-[auto_1fr] gap-2 px-3 py-2.5", filmEnter)}>
              <div className="flex items-center justify-between">
                <span className="font-medium text-foreground">Your apps</span>
                <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
                  <span className="grid size-4 place-items-center rounded-full bg-primary text-[8px] font-semibold text-white">A</span>
                  ana@yourapp.com
                </span>
              </div>
              <div className="grid grid-cols-2 gap-1.5">
                {tiles.map((t) => {
                  const Icon = t.icon;
                  const updated = t.name === "Approvals" && at("update");
                  return (
                    <span
                      key={t.name}
                      className={cn(
                        "relative flex items-center gap-1.5 rounded-2xl px-2 py-1.5 text-[10.5px] ring-1 transition-[box-shadow] duration-500",
                        t.locked ? "bg-foreground/[0.03] text-muted-foreground ring-foreground/6" : "bg-background text-foreground ring-foreground/8",
                        t.locked && name === "locked" && "ring-destructive/40",
                        updated && "ring-primary/50",
                      )}
                    >
                      <Icon className={cn("size-3.5 shrink-0", !t.locked && "text-primary")} />
                      <span className="truncate">{t.name}</span>
                      {t.locked && <LockKeyhole className="ml-auto size-3 shrink-0" />}
                      {updated && <span className={cn("ml-auto rounded-full bg-primary px-1 text-[8.5px] font-semibold text-white", filmEnter)}>v2</span>}
                    </span>
                  );
                })}
              </div>
            </div>
          )}
        </div>
        <Caption lines={itCaption} at={still ? 3 : IT.findIndex(([n]) => n === name)} />
      </div>
    </div>
  );
}

// --- FDEs and implementation partners: "One client project, and what
// happens after launch." The client's app is deployed and delivered; then
// the three ways on, in turn, and who does what in each: hand it over,
// maintain it, or offer it as a managed service under your own brand.

const FDE = [
  ["deploying", 1600],
  ["live", 1200],
  ["hand-over", 3000],
  ["maintain", 3000],
  ["managed", 3000],
] as const;

const models = [
  { key: "hand-over", tab: "Hand over", runs: "Client", servers: "Client", line: "The client takes over, with shpyrd" },
  { key: "maintain", tab: "Maintain", runs: "You", servers: "Client", line: "You keep it running; the client pays" },
  { key: "managed", tab: "Managed", runs: "You", servers: "Your brand", line: "Your brand, your prices" },
] as const;

function Who({ who }: { who: string }) {
  const you = who !== "Client";
  return (
    <span className={cn("inline-flex items-center gap-1.5 font-medium text-foreground", filmEnter)}>
      <span className={cn("grid size-4 place-items-center rounded-full text-[8px] font-semibold", you ? "bg-primary text-white" : "bg-foreground/80 text-background")}>
        {you ? "Y" : "C"}
      </span>
      {who}
    </span>
  );
}

export function PartnersFilm() {
  const { root, name, past, round, still } = useFilm(FDE);
  const live = still || past("live");
  const model = still ? 2 : models.findIndex((m) => m.key === name);
  const shown = models[Math.max(model, 0)];

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className="flex size-full flex-col gap-2">
        {/* The client's project, deployed. */}
        <div className={cn(filmPiece, "flex items-center justify-between px-3 py-2")}>
          <span className="font-medium text-foreground">yourclient.com</span>
          {live ? (
            <span className={cn("inline-flex items-center gap-1 rounded-full bg-success/12 px-2 py-0.5 text-[10px] font-medium text-success", filmEnter)}>
              <Check className="size-3" />
              Delivered
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
              <span className="size-2.5 animate-spin rounded-full border-[1.5px] border-foreground/15 border-t-primary" />
              Deploying
            </span>
          )}
        </div>

        {/* After launch: the three ways on. */}
        <div className={cn("grid flex-1 content-start gap-2 transition-opacity duration-500", model >= 0 ? "opacity-100" : "opacity-40")}>
          <span className="text-[10px] text-muted-foreground">After launch</span>
          <div className="grid grid-cols-3 gap-1 rounded-full bg-foreground/[0.05] p-1 text-[10.5px] font-medium">
            {models.map((m, i) => (
              <span
                key={m.key}
                className={cn(
                  "rounded-full py-1 text-center transition-[background-color,color,box-shadow] duration-500",
                  i === model ? "bg-background text-primary shadow-[0_1px_3px_rgb(0_0_0/0.1)] ring-1 ring-foreground/8" : "text-muted-foreground",
                )}
              >
                {m.tab}
              </span>
            ))}
          </div>
          {/* Who does what. */}
          <div className="grid gap-1.5 rounded-2xl bg-foreground/[0.04] px-3 py-2 text-[10.5px]">
            <span className="flex items-center justify-between">
              <span className="text-muted-foreground">Runs the app</span>
              {model >= 0 ? <Who key={`r${model}`} who={shown.runs} /> : <span className="text-muted-foreground">–</span>}
            </span>
            <span className="flex items-center justify-between">
              <span className="text-muted-foreground">Pays the servers</span>
              {model >= 0 ? <Who key={`s${model}`} who={shown.servers} /> : <span className="text-muted-foreground">–</span>}
            </span>
          </div>
        </div>

        <Caption lines={["Deploy the client's project", ...models.map((m) => m.line)]} at={model + 1} />
      </div>
    </div>
  );
}
