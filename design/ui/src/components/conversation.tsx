import * as React from "react";
import { Check, Circle, X } from "lucide-react";
import { cn } from "cn";

// A person and an agent, taking turns: what was asked, what the agent did about
// it, and what it said back. The steps are shown between the two, one a line,
// because they are what the reader has to believe.

function Conversation({ className, ...props }: React.ComponentProps<"ol">) {
  return (
    <ol data-slot="conversation" className={cn("grid gap-4", className)} {...props} />
  );
}

const marks = {
  done: { icon: Check, className: "text-success" },
  pending: { icon: Circle, className: "text-muted-foreground" },
  failed: { icon: X, className: "text-destructive" },
} as const;

export type ConversationStep = {
  label: React.ReactNode;
  status?: keyof typeof marks;
};

// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function ConversationMessage({
  className,
  from,
  author,
  steps,
  children,
  ...props
}: React.ComponentProps<"li"> & {
  // A person's turn sits to the right, in a bubble; an agent's to the left,
  // on the page.
  from: "person" | "agent";
  // Who is speaking, over the message.
  author: React.ReactNode;
  // What the agent did before it answered.
  steps?: ConversationStep[];
}) {
  const person = from === "person";

  return (
    <li
      data-slot="conversation-message"
      data-from={from}
      className={cn("grid gap-2", person ? "justify-items-end" : "justify-items-start", className)}
      {...props}
    >
      <span data-slot="conversation-author" className="text-xs text-muted-foreground">
        {author}
      </span>

      {steps && steps.length > 0 && (
        <ul
          data-slot="conversation-steps"
          className="grid w-full gap-1.5 rounded-lg border bg-muted/50 px-3 py-2.5 font-mono text-xs"
        >
          {steps.map((step, i) => {
            const mark = marks[step.status ?? "done"];
            const Icon = mark.icon;
            return (
              <li key={i} className="flex items-start gap-2">
                <Icon aria-hidden={true} className={cn("mt-px size-3.5 shrink-0", mark.className)} />
                <span className="min-w-0">{step.label}</span>
              </li>
            );
          })}
        </ul>
      )}

      <div
        data-slot="conversation-body"
        className={cn(
          "max-w-[46ch] text-sm",
          person && "rounded-2xl rounded-br-sm bg-muted px-4 py-2.5",
        )}
      >
        {children}
      </div>
    </li>
  );
}

export { Conversation, ConversationMessage };
