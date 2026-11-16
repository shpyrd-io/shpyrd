"use client";

// The picture beside Easy Deployment's heading (Giovani, 2026-11-12: more
// didactic, as the home's hero and the manifesto tell theirs): the three
// steps the page names, played out in order, in the home hero's chat and
// the site's browser window. Over them the three steps, the one being shown
// lit, so the picture reads as the list under it.
//
// 1. Put it online: "Publish this app." The agent deploys; the app opens in
//    the browser at its address, its version beside it.
// 2. Share the link: "Use app.yourapp.com." The address becomes the domain.
// 3. Change it anytime: "Make the Approve button orange." A new version goes
//    live; "Undo that." and the one before is back.
import { useEffect, useState } from "react";
import { Check, Rocket, Share2, Sparkles, Undo2 } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Conversation, ConversationMessage, type ConversationStep } from "@shpyrd/ui/components/conversation";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Composer, Typing, Url } from "@/components/shipping-chat";
import { Screen } from "@/components/screen";

const ai = "Your favorite AI";
const asks = {
  publish: "Publish this app.",
  domain: "Use app.yourapp.com.",
  change: "Make the Approve button orange.",
  undo: "Undo that.",
};

// Each moment long enough to read before the next (about 34s in all).
const MOMENTS = [
  ["draft-publish", 2400],
  ["sent-publish", 1200],
  ["deploying", 2600],
  ["live", 4200],
  ["draft-domain", 2200],
  ["sent-domain", 1800],
  ["domain", 4000],
  ["draft-change", 2600],
  ["sent-change", 1200],
  ["releasing", 2400],
  ["changed", 3600],
  ["draft-undo", 1600],
  ["sent-undo", 1200],
  ["undone", 5000],
] as const;
type Moment = (typeof MOMENTS)[number][0];
const at = (name: Moment) => MOMENTS.findIndex(([n]) => n === name);
const LAST = MOMENTS.length - 1;

// The three steps, as the page names them under the picture.
const steps = [
  { icon: <Rocket />, name: "Put it online", from: "draft-publish" },
  { icon: <Share2 />, name: "Share the link", from: "draft-domain" },
  { icon: <Undo2 />, name: "Change it anytime", from: "draft-change" },
] as const;

const enter = "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// The app itself, the same in every version but for its button: a list of
// expense claims to approve.
function Expenses({ orange }: { orange: boolean }) {
  const claims = [
    ["Team offsite", "$1,240.00"],
    ["Laptop stand", "$89.90"],
    ["Client dinner", "$312.50"],
  ];
  return (
    <div className="grid gap-3 p-4 text-xs">
      <div className="flex items-center justify-between">
        <span className="font-heading text-sm font-semibold text-foreground">Expense claims</span>
        <span className="text-[11px] text-muted-foreground">3 waiting</span>
      </div>
      <div className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-background dark:bg-card">
        {claims.map(([what, amount]) => (
          <div key={what} className="flex items-center gap-3 px-3 py-2">
            <span className="flex-1 text-foreground">{what}</span>
            <span className="text-foreground tabular-nums">{amount}</span>
            <span
              className={cn(
                "rounded-md px-2 py-1 text-[11px] font-medium transition-colors duration-700 ease-out",
                orange ? "bg-primary text-primary-foreground" : "bg-foreground/[0.07] text-foreground",
              )}
            >
              Approve
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function DeployStory() {
  const [moment, setMoment] = useState(0);
  const [round, setRound] = useState(0);
  // Rendered when the site is built; the reader's preference is read in an
  // effect. For who asked for less motion it stays on its last moment.
  const [still, setStill] = useState<boolean | null>(null);
  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setStill(less);
    if (less) setMoment(LAST);
  }, []);
  useEffect(() => {
    if (still !== false) return;
    const timer = window.setTimeout(() => {
      if (moment < LAST) return setMoment(moment + 1);
      setRound((r) => r + 1);
      setMoment(0);
    }, MOMENTS[moment][1]);
    return () => window.clearTimeout(timer);
  }, [moment, still]);

  const name = MOMENTS[moment][0];
  const past = (m: Moment) => moment >= at(m);
  const draft = name.startsWith("draft-") ? asks[name.slice(6) as keyof typeof asks] : null;
  const live = past("live");
  const domain = past("domain");
  const changed = past("changed") && !past("undone");
  const version = past("undone") ? 1 : past("changed") ? 2 : 1;
  const step = past("draft-change") ? 2 : past("draft-domain") ? 1 : 0;

  const agent = (
    <span className="inline-flex items-center gap-1.5">
      <Sparkles aria-hidden="true" className="size-3 text-primary" />
      {ai}
    </span>
  );
  const ran = (label: string, done: boolean): ConversationStep[] => [{ label, status: done ? "done" : "pending" }];

  // Only the last exchanges, as a chat scrolls: the newest at the foot.
  const messages: React.ReactNode[] = [];
  if (past("sent-publish"))
    messages.push(
      <ConversationMessage key="p" from="person" author="You" className={enter}>
        {asks.publish}
      </ConversationMessage>,
      <ConversationMessage key="pa" from="agent" author={agent} steps={ran("shpyrd deploy · release 1", live)} className={enter}>
        {live ? (
          <span>
            It&apos;s live at <Url>https://expenses.yourapp.com</Url>
          </span>
        ) : (
          <Typing />
        )}
      </ConversationMessage>,
    );
  if (past("sent-domain"))
    messages.push(
      <ConversationMessage key="d" from="person" author="You" className={enter}>
        {asks.domain}
      </ConversationMessage>,
      <ConversationMessage key="da" from="agent" author={agent} className={enter}>
        {domain ? (
          <span>
            Done. Share <Url>https://app.yourapp.com</Url>
          </span>
        ) : (
          <Typing />
        )}
      </ConversationMessage>,
    );
  if (past("sent-change"))
    messages.push(
      <ConversationMessage key="c" from="person" author="You" className={enter}>
        {asks.change}
      </ConversationMessage>,
      <ConversationMessage key="ca" from="agent" author={agent} steps={ran("shpyrd deploy · release 2", past("changed"))} className={enter}>
        {past("changed") ? <span>Version 2 is live.</span> : <Typing />}
      </ConversationMessage>,
    );
  if (past("sent-undo"))
    messages.push(
      <ConversationMessage key="u" from="person" author="You" className={enter}>
        {asks.undo}
      </ConversationMessage>,
      <ConversationMessage key="ua" from="agent" author={agent} steps={ran("shpyrd rollback · release 1", past("undone"))} className={enter}>
        {past("undone") ? <span>Back on version 1.</span> : <Typing />}
      </ConversationMessage>,
    );

  return (
    <div className="grid gap-4">
      {/* The three steps, the one being shown lit: the same as the page's
          list under the picture. */}
      <ol className="flex flex-wrap justify-center gap-2 text-xs font-medium @3xl/hero:justify-start">
        {steps.map((s, i) => (
          <li
            key={s.name}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 ring-1 transition-[background-color,color,box-shadow] duration-500 ease-out [&_svg]:size-3.5",
              i === step
                ? "bg-primary text-primary-foreground ring-primary shadow-[inset_0_1px_0_rgb(255_255_255/0),0_6px_16px_-6px_rgb(255_79_0/0.5)]"
                : i < step
                  ? "bg-background text-foreground ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_1px_2px_rgb(20_20_30/0.05)] dark:bg-card [&_svg]:text-primary"
                  : "bg-background text-muted-foreground ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_1px_2px_rgb(20_20_30/0.05)] dark:bg-card",
            )}
          >
            {i < step ? <Check /> : s.icon}
            {i + 1}. {s.name}
          </li>
        ))}
      </ol>

      <div className="relative grid gap-4 md:block md:h-[34rem]">
        {/* The app, in the browser, at the top right, from the first deploy:
            its address, then the domain; its version beside it. */}
        <div
          className={cn(
            "relative transition-[opacity,translate] duration-700 ease-out md:absolute md:top-0 md:right-0 md:w-[78%]",
            live ? "translate-y-0 opacity-100" : "translate-y-3 opacity-0",
          )}
        >
          <Screen address={domain ? "app.yourapp.com" : "expenses.yourapp.com"}>
            <Expenses orange={changed} />
          </Screen>
          <span
            className={cn(
              "absolute -top-3 right-5 inline-flex items-center gap-2 rounded-full bg-background px-3 py-1 text-xs font-medium text-foreground shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8 dark:bg-card",
            )}
          >
            <span className="relative grid size-2 place-items-center">
              <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-70" />
              <span className="relative size-2 rounded-full bg-success" />
            </span>
            Live · v{version}
          </span>
        </div>

        {/* The chat, in front, at the bottom left. */}
        <div className={cn(glass, "relative z-20 rounded-2xl p-2.5 md:absolute md:bottom-0 md:left-0 md:w-[70%]")}>
          <AppWindow
            title={agent}
            detail="~/projects/expenses"
            className={cn(
              "h-[17rem] rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
              "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
            )}
            footer={<Composer text={draft} placeholder="Message your AI…" />}
          >
            <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-4 py-4 text-sm [mask-image:linear-gradient(to_bottom,transparent,black_3rem)]">
              <Conversation key={round}>{messages}</Conversation>
            </div>
          </AppWindow>
        </div>
      </div>
    </div>
  );
}
