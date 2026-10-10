"use client";

// Slide 5: from the prompt to the team. The home page's chat window plays a
// conversation with the reader's agent, message after message, and the step
// it is on lights up beside it.
import { useEffect, useState } from "react";
import { Bot, MessageSquareText, Play, Rocket, Share2, Undo2 } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Button } from "@shpyrd/ui/components/button";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { flow } from "@shpyrd/content/site/tour";
import { AgentMark, useCurrentAgent } from "@/components/add-to-agent";
import { Composer, Typing, Url } from "@/components/shipping-chat";
import { Intro, Label, panel } from "./parts";

const icons = [<MessageSquareText key="0" />, <Bot key="1" />, <Rocket key="2" />, <Share2 key="3" />, <Undo2 key="4" />];

// The beats the window plays: a person's message arrives; an agent's message
// types, then says what it did, its commands still running, then done.
type Beat = { message: number; phase: "sent" | "typing" | "running" | "done" };
const beats: Beat[] = flow.messages.flatMap((m, i): Beat[] =>
  m.from === "person"
    ? [{ message: i, phase: "sent" }]
    : [{ message: i, phase: "typing" }, ...(m.runs ? [{ message: i, phase: "running" } as Beat] : []), { message: i, phase: "done" }],
);
const BEAT = 1100;
// How long the conversation stays on each step: its beats, end to end.
const stepTime = (step: number) => beats.filter((b) => flow.messages[b.message].step === step).length * BEAT;
const enter = "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// A message's addresses, drawn as the chat draws them.
function words(text: string) {
  return text.split(/(https?:\/\/\S+?)(?=[,.]?(?:\s|$))/).map((part, i) => (i % 2 ? <Url key={i}>{part}</Url> : part));
}

export function Flow() {
  const { agent } = useCurrentAgent();
  const [beat, setBeat] = useState(0);
  const [run, setRun] = useState(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setBeat(beats.length - 1);
    setBeat(0);
    const timer = window.setInterval(
      () => setBeat((b) => (b >= beats.length - 1 ? (window.clearInterval(timer), b) : b + 1)),
      BEAT,
    );
    return () => window.clearInterval(timer);
  }, [run]);

  const now = beats[beat];
  const playing = beat < beats.length - 1;
  const step = flow.messages[now.message].step;
  // How far each message has got: not yet, or its latest beat.
  const phaseOf = (i: number) => {
    if (i > now.message) return null;
    if (i < now.message) return "done";
    return now.phase;
  };

  return (
    <div className="grid gap-8">
      <Intro label={<Label>{flow.label}</Label>} heading={flow.heading} lead={flow.lead} />

      <div className="grid items-start gap-5 lg:grid-cols-[18rem_1fr]">
        <ol className="grid gap-2">
          {flow.steps.map((s, i) => (
            <li
              key={s.title}
              aria-current={i === step ? "step" : undefined}
              className={cn(
                panel,
                "relative flex items-center gap-3 overflow-hidden px-3 py-2.5 transition-[opacity,border-color,background-color] duration-normal ease-move",
                i === step && "border-primary/50 bg-primary/5",
                i > step && "opacity-45",
              )}
            >
              <span
                className={cn(
                  "grid size-9 shrink-0 place-items-center rounded-lg transition-colors duration-normal [&_svg]:size-4",
                  i <= step ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground",
                )}
              >
                {icons[i]}
              </span>
              <span className="grid">
                <span className="font-medium">
                  <span className="mr-1.5 text-muted-foreground tabular-nums">{i + 1}</span>
                  {s.title}
                </span>
                <span className="text-xs text-muted-foreground">{s.body}</span>
              </span>
              {/* How long until the next step. */}
              {i === step && playing && (
                <span aria-hidden="true" className="absolute inset-y-2 right-2 w-1 overflow-hidden rounded-full bg-primary/15">
                  <span
                    key={`${run}-${step}`}
                    className="block w-full rounded-full bg-primary motion-safe:animate-[tour-fill_linear_forwards]"
                    style={{ animationDuration: `${stepTime(step)}ms` }}
                  />
                </span>
              )}
            </li>
          ))}
        </ol>

        {/* The window of the home page's chat (shipping-chat.tsx). */}
        <div className={cn(glass, "rounded-[18px] p-2.5")}>
          <AppWindow
            title={
              <span className="inline-flex items-center gap-1.5">
                <AgentMark id={agent.id} className="size-3.5" />
                {agent.name}
              </span>
            }
            detail={flow.window}
            className={cn(
              "h-[26rem] rounded-[10px] border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
              "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
            )}
            footer={
              <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                  <Composer text={null} placeholder={`Message ${agent.name}…`} />
                </div>
                <Button variant="ghost" size="sm" icon={<Play />} onClick={() => setRun((r) => r + 1)}>
                  {flow.replay}
                </Button>
              </div>
            }
          >
            <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-5 py-5 [mask-image:linear-gradient(to_bottom,transparent,black_3rem)]">
              <Conversation key={run}>
                {flow.messages.map((m, i) => {
                  const phase = phaseOf(i);
                  if (!phase) return null;
                  if (m.from === "person")
                    return (
                      <ConversationMessage key={i} from="person" author="You" className={enter}>
                        {m.text}
                      </ConversationMessage>
                    );
                  return (
                    <ConversationMessage
                      key={i}
                      from="agent"
                      author={
                        <span className="inline-flex items-center gap-1.5">
                          <AgentMark id={agent.id} className="size-3" />
                          {agent.name}
                        </span>
                      }
                      steps={
                        m.runs && phase !== "typing"
                          ? m.runs.map((label) => ({ label: <code className="font-mono text-xs">{label}</code>, status: phase === "running" ? "pending" : "done" }))
                          : undefined
                      }
                      className={enter}
                    >
                      {phase === "done" ? <span>{words(m.text)}</span> : <Typing />}
                    </ConversationMessage>
                  );
                })}
              </Conversation>
            </div>
          </AppWindow>
        </div>
      </div>
    </div>
  );
}
