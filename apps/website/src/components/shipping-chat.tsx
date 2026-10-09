"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowUp } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Conversation, ConversationMessage, type ConversationStep } from "@shpyrd/ui/components/conversation";
import { agents } from "@shpyrd/content/site/agents";
import { AgentMark, clock, useCurrentAgent } from "@/components/add-to-agent";

// The homepage's picture in proposal 5: the agent's own app, where someone asks
// for an app, then asks for it to be shipped and shared, and watches it happen.
// Each thing they say is typed in the box at the bottom first, then sent.
//
// The agent is the one the "Add to" buttons are showing, and changes when they
// do, in the middle of a conversation too: it works the same with any of them.
// The ask to ship changes from one conversation to the next; there is more
// than one way to say it.
//
// Everything the agent does is what the shpyrd CLI does today, run by the agent.

const ask = "Build me a CRM to manage my contacts and deals. Don't forget a nice kanban!";
const ship = ["Put it on the internet.", "Send it to the cloud.", "Put it on the cloud."];
const share = "Share it with the Sales team.";

// The moments of one conversation, in order, and how long each lasts. A
// "draft" is the next message being typed in the box.
const MOMENTS = [
  ["draft-ask", 2600],
  ["sent-ask", 900],
  ["thinking-build", 1800],
  ["built", 2200],
  ["draft-ship", 1600],
  ["sent-ship", 900],
  ["thinking-ship", 1000],
  ["deploying", 1300],
  ["going-live", 1300],
  ["shipped", 2200],
  ["draft-share", 1600],
  ["sent-share", 900],
  ["thinking-share", 1000],
  ["sharing", 1200],
  ["shared", 5000],
] as const;
type Moment = (typeof MOMENTS)[number][0];
const at = (name: Moment) => MOMENTS.findIndex(([n]) => n === name);
const LAST = MOMENTS.length - 1;

function Typing() {
  return (
    <span className="inline-flex gap-1 py-1" aria-label="Typing">
      {[0, 150, 300].map((delay) => (
        <span
          key={delay}
          className="size-1.5 animate-bounce rounded-full bg-muted-foreground"
          style={{ animationDelay: `${delay}ms` }}
        />
      ))}
    </span>
  );
}

// The box at the bottom, with the message being typed into it, a letter at a
// time over most of the moment it is drafted in.
// A message longer than the box scrolls along with the caret, as a real input
// does, so the end being typed stays in view; an empty box shows its start.
export function Composer({ text, placeholder }: { text: string | null; placeholder: string }) {
  const [shown, setShown] = useState(0);
  const line = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const el = line.current;
    if (el) el.scrollLeft = text ? el.scrollWidth : 0;
  }, [shown, text]);
  useEffect(() => {
    setShown(0);
    if (!text) return;
    const timer = window.setInterval(() => {
      setShown((n) => (n >= text.length ? n : n + 1));
    }, 28);
    return () => window.clearInterval(timer);
  }, [text]);

  return (
    <div className="flex items-center gap-2 rounded-control border border-foreground/8 bg-background/80 px-3 py-2 text-sm shadow-[0_1px_2px_rgb(0_0_0/0.04)]">
      <span
        ref={line}
        className={`min-w-0 flex-1 overflow-hidden whitespace-nowrap ${text ? "" : "truncate text-muted-foreground"}`}
      >
        {/* Each letter typed is a new element rather than a change to the
            text: a browser translating the page replaces the text it finds,
            so React would type into text no longer on the page and remove
            what is not there, which stops the page. */}
        <span key={text ? shown : "placeholder"}>{text ? text.slice(0, shown) : placeholder}</span>
        {text && <span className="ml-px inline-block h-4 w-px translate-y-0.5 animate-pulse bg-foreground" />}
      </span>
      <span
        aria-hidden="true"
        className={`grid size-6 shrink-0 place-items-center rounded-md ${
          text && shown >= text.length ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
        }`}
      >
        <ArrowUp className="size-3.5" />
      </span>
    </div>
  );
}

const enter = "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// What the agent says replaces its dots in an element of its own, never as bare
// text beside them: a browser translating the page moves bare text into
// elements of its own, and React, removing what it put there, would stop the
// page.
//
// An address in what the agent says, looking like the link it would be. It is
// made up, so it goes nowhere.
function Url({ children }: { children: string }) {
  return (
    <span className="font-medium break-all text-foreground underline decoration-muted-foreground/60 underline-offset-2">
      {children}
    </span>
  );
}

export function ShippingChat() {
  const { index } = useCurrentAgent();
  const [moment, setMoment] = useState(0);
  const [round, setRound] = useState(0);
  // Rendered when the application is built; the reader's preference is read in
  // an effect. Until then, and for who asked for less motion, it is still.
  const [still, setStill] = useState<boolean | null>(null);

  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setStill(less);
    if (less) setMoment(LAST);
  }, []);

  // Each agent tells the whole conversation in its own window; when it ends,
  // the next agent comes forward, and the "Add to" buttons with it. While the
  // chat moves, it drives their clock rather than their own interval.
  useEffect(() => (still === false ? clock.drive() : undefined), [still]);

  useEffect(() => {
    if (still !== false) return;
    const timer = window.setTimeout(() => {
      if (moment < LAST) return setMoment(moment + 1);
      clock.next();
      setRound((r) => r + 1);
      setMoment(0);
    }, MOMENTS[moment][1]);
    return () => window.clearTimeout(timer);
  }, [moment, still]);

  // Someone picked an agent on a button: its window starts the conversation.
  const shown = useRef(index);
  useEffect(() => {
    if (shown.current === index) return;
    shown.current = index;
    if (still === false) setMoment(0);
  }, [index, still]);

  const past = (name: Moment) => moment >= at(name);
  const says = ship[round % ship.length];
  const draft =
    MOMENTS[moment][0] === "draft-ask"
      ? ask
      : MOMENTS[moment][0] === "draft-ship"
        ? says
        : MOMENTS[moment][0] === "draft-share"
          ? share
          : null;

  type Agent = (typeof agents)[number];
  const name = (agent: Agent, size: string) => (
    <span className="inline-flex items-center gap-1.5">
      <AgentMark id={agent.id} className={size} />
      {agent.name}
    </span>
  );

  const deploy: ConversationStep[] = [
    { label: "shpyrd deploy · release 1", status: past("going-live") ? "done" : "pending" },
    ...(past("going-live")
      ? [
          {
            label: "crm.acme.shpyrd.app is live · sign-in required",
            status: past("shipped") ? "done" : "pending",
          } as const,
        ]
      : []),
  ];

  // One window for each agent, the same conversation in all: the agent the
  // "Add to" buttons name is in front and in focus; the others wait behind it,
  // smaller and out of focus, and come forward in turn.
  const windowOf = (agent: Agent) => (
    // After aniwall.aura.build's showcase: the app sits in a frame of frosted
    // glass, the window itself light, its edges drawn in white rather than grey.
    <div className={cn(glass, "rounded-[18px] p-2.5")}>
    <AppWindow
      title={name(agent, "size-3.5")}
      detail="~/projects/crm"
      className={cn(
        "h-[34rem] rounded-[10px] border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
        "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
      )}
      footer={<Composer text={draft} placeholder={`Message ${agent.name}…`} />}
    >
      <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-5 py-5 [mask-image:linear-gradient(to_bottom,transparent,black_3rem)]">
        <Conversation key={round}>
          {past("sent-ask") && (
            <ConversationMessage from="person" author="You" className={enter}>
              {ask}
            </ConversationMessage>
          )}

          {past("thinking-build") && (
            <ConversationMessage from="agent" author={name(agent, "size-3")} className={enter}>
              {past("built") ? (
                <span>
                  Your CRM is built! Contacts, deals, and a kanban you can drag them across.
                  It&apos;s running at <Url>http://localhost:3000</Url>
                </span>
              ) : (
                <Typing />
              )}
            </ConversationMessage>
          )}

          {past("sent-ship") && (
            <ConversationMessage from="person" author="You" className={enter}>
              {says}
            </ConversationMessage>
          )}

          {past("thinking-ship") && (
            <ConversationMessage
              from="agent"
              author={name(agent, "size-3")}
              steps={past("deploying") ? deploy : undefined}
              className={enter}
            >
              {past("shipped") ? (
                <span>
                  Done. It&apos;s live at <Url>https://crm.acme.shpyrd.app</Url>, behind your work
                  sign-in. Only you can open it for now.
                </span>
              ) : (
                <Typing />
              )}
            </ConversationMessage>
          )}

          {past("sent-share") && (
            <ConversationMessage from="person" author="You" className={enter}>
              {share}
            </ConversationMessage>
          )}

          {past("thinking-share") && (
            <ConversationMessage
              from="agent"
              author={name(agent, "size-3")}
              steps={
                past("sharing")
                  ? [{ label: "Sales · can use", status: past("shared") ? "done" : "pending" }]
                  : undefined
              }
              className={enter}
            >
              {past("shared") ? (
                <span>
                  Done. Sales can open <Url>https://crm.acme.shpyrd.app</Url>, and will find it
                  among their apps next time they sign in.
                </span>
              ) : (
                <Typing />
              )}
            </ConversationMessage>
          )}
        </Conversation>
      </div>
    </AppWindow>
    </div>
  );

  return (
    <div className="relative">
      {agents.map((agent, k) => {
        const depth = (k - index + agents.length) % agents.length;
        return (
          <div
            key={agent.id}
            aria-hidden={depth > 0 || undefined}
            inert={depth > 0 || undefined}
            className={cn(
              "transition-[translate,scale,opacity,filter] duration-700 ease-move",
              depth === 0 ? "relative z-30" : "pointer-events-none absolute inset-0 max-md:hidden",
              // Scattered around the front one like windows on a desktop, far
              // enough out of focus that only its shape and its colour read.
              depth === 1 && "z-20 -translate-x-[28%] -translate-y-[12%] scale-[0.82] opacity-80 blur-[7px]",
              depth === 2 && "z-15 translate-x-[28%] -translate-y-[20%] scale-[0.76] opacity-70 blur-[9px]",
              depth === 3 && "z-10 -translate-x-[22%] translate-y-[20%] scale-[0.7] opacity-60 blur-[11px]",
              depth === 4 && "z-5 translate-x-[34%] translate-y-[16%] scale-[0.66] opacity-50 blur-[12px]",
            )}
          >
            {windowOf(agent)}
          </div>
        );
      })}
    </div>
  );
}
