"use client";

import { Sparkles } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Conversation, ConversationMessage, type ConversationStep } from "@shpyrd/ui/components/conversation";
import { cn } from "@shpyrd/ui/lib/cn";
import { Composer, Typing } from "@/components/shipping-chat";
import { FilmFrame, filmWindow, useFilm } from "@/components/film";

// The film of the feature "Your AI keeps it running" (Giovani, 2026-10-19):
// a chat with your AI in the window of the hero's chat. You ask why checkout
// is slow; it calls shpyrd's tools one after the other (each running, then
// done), finds the bad release and offers to roll it back; you say yes; it
// rolls back and says what it did. It plays only while on the screen and
// starts again; still, for who asked for less motion, it shows the end.

const ai = "Your favorite AI";
const ask = "Why is checkout slow today?";
const yes = "Yes, roll it back.";
const calls = ["shpyrd releases", "get_metrics yourapp.com", "shpyrd logs yourapp.com"];

const MOMENTS = [
  ["draft-ask", 1700],
  ["sent-ask", 700],
  ["call-1", 900],
  ["call-2", 900],
  ["call-3", 900],
  ["answer", 2600],
  ["draft-yes", 1100],
  ["sent-yes", 600],
  ["rolling", 1300],
  ["done", 3800],
] as const;

const enter = "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

export function AgentChatFilm() {
  const { root, name, past, round } = useFilm(MOMENTS);
  const draft = name === "draft-ask" ? ask : name === "draft-yes" ? yes : null;

  // The calls made so far: the last one running until the next starts.
  const made = past("call-3") ? 3 : past("call-2") ? 2 : past("call-1") ? 1 : 0;
  const tools: ConversationStep[] = calls.slice(0, made).map((label, i) => ({
    label,
    status: i < made - 1 || past("answer") ? "done" : "pending",
  }));
  const rollback: ConversationStep[] = [{ label: "shpyrd rollback · v41", status: past("done") ? "done" : "pending" }];

  const agent = (
    <span className="inline-flex items-center gap-1.5">
      <Sparkles aria-hidden="true" className="size-3 text-primary" />
      {ai}
    </span>
  );

  return (
    <FilmFrame root={root}>
      <AppWindow
        // No folder beside the name: the card is narrow, the name stays on
        // one line.
        title={agent}
        className={cn(
          filmWindow,
          "[&_[data-slot=app-window-bar]]:gap-2 [&_[data-slot=app-window-bar]]:py-1.5 [&_[data-slot=app-window-bar]]:text-[11px] [&_[data-slot=app-window-bar]]:whitespace-nowrap [&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6 [&_[data-slot=app-window-footer]]:p-1.5 [&_[data-slot=app-window-footer]>div]:py-1.5 [&_[data-slot=app-window-footer]>div]:text-xs",
        )}
        footer={<Composer text={draft} placeholder="Message your AI…" />}
      >
        <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-3 py-2.5 [mask-image:linear-gradient(to_bottom,transparent,black_2rem)]">
          <Conversation
            key={round}
            className={cn(
              "gap-3 text-foreground",
              "[&_[data-slot=conversation-body]]:text-xs [&_[data-slot=conversation-body]]:leading-relaxed",
              "[&_[data-from=person]_[data-slot=conversation-body]]:bg-foreground/[0.06] [&_[data-from=person]_[data-slot=conversation-body]]:px-3 [&_[data-from=person]_[data-slot=conversation-body]]:py-1.5",
              "[&_[data-slot=conversation-steps]]:gap-1 [&_[data-slot=conversation-steps]]:border-foreground/8 [&_[data-slot=conversation-steps]]:bg-foreground/[0.03] [&_[data-slot=conversation-steps]]:px-2.5 [&_[data-slot=conversation-steps]]:py-2 [&_[data-slot=conversation-steps]]:text-[11px] [&_[data-slot=conversation-steps]]:text-muted-foreground",
              "[&_[data-slot=conversation-author]]:text-[11px]",
            )}
          >
            {past("sent-ask") && (
              <ConversationMessage from="person" author="You" className={enter}>
                {ask}
              </ConversationMessage>
            )}
            {past("call-1") && (
              <ConversationMessage from="agent" author={agent} steps={tools} className={enter}>
                {past("answer") ? (
                  <span className={enter}>
                    p95 went from 180 ms to 2.4 s after release v42. Roll back to v41?
                  </span>
                ) : (
                  <Typing />
                )}
              </ConversationMessage>
            )}
            {past("sent-yes") && (
              <ConversationMessage from="person" author="You" className={enter}>
                {yes}
              </ConversationMessage>
            )}
            {past("rolling") && (
              <ConversationMessage from="agent" author={agent} steps={rollback} className={enter}>
                {past("done") ? (
                  <span className={enter}>
                    Done. Back on v41, and checkout answers in <span className="font-medium text-success">180 ms</span> again.
                  </span>
                ) : (
                  <Typing />
                )}
              </ConversationMessage>
            )}
          </Conversation>
        </div>
      </AppWindow>
    </FilmFrame>
  );
}
