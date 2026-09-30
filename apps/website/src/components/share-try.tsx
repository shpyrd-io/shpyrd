"use client";

import { useState } from "react";
import { Check, Plus } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";

// Sharing, tried: tap who the app is for, and the sentence to the agent writes
// itself, with the agent's answer under it. There is nothing else to set.

const people = [
  { id: "ana", said: "Ana" },
  { id: "joao", said: "João" },
  { id: "rita", said: "Rita" },
  { id: "finance", said: "the Finance team" },
  { id: "everyone", said: "everyone at Acme" },
];

// "Ana", "Ana and João", "Ana, João and Rita".
function list(words: string[]) {
  if (words.length < 2) return words.join("");
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`;
}

export function ShareTry() {
  const [chosen, setChosen] = useState<string[]>(["ana", "finance"]);
  const said = people.filter((p) => chosen.includes(p.id)).map((p) => p.said);

  function toggle(id: string) {
    setChosen((c) => (c.includes(id) ? c.filter((x) => x !== id) : [...c, id]));
  }

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap justify-center gap-2" role="group" aria-label="Who it is for">
        {people.map((p) => {
          const on = chosen.includes(p.id);
          return (
            <Button
              key={p.id}
              variant={on ? "secondary" : "outline"}
              aria-pressed={on}
              icon={on ? <Check /> : <Plus />}
              className="rounded-full"
              onClick={() => toggle(p.id)}
            >
              {p.said}
            </Button>
          );
        })}
      </div>

      <Card className="px-6 py-6" aria-live="polite">
        {said.length === 0 ? (
          <p className="text-center text-sm text-muted-foreground">
            Pick who the app is for.
          </p>
        ) : (
          <Conversation>
            <ConversationMessage from="person" author="You">
              Share the purchase tracker with {list(said)}.
            </ConversationMessage>
            <ConversationMessage from="agent" author="Your agent">
              Done. {said.length === 1 ? "They" : "They all"} can open it at{" "}
              <strong>purchases.acme.shpyrd.app</strong> with their work account. Nobody else can.
            </ConversationMessage>
          </Conversation>
        )}
      </Card>
    </div>
  );
}
