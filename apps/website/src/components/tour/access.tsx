"use client";

// Slide 6: the same app, three people. Left alone, the next person opens it
// every few seconds, a bar on the one whose turn it is filling until the
// next; choosing one stops there. What their browser gets, what the app is
// told about them and the verdict change with them.
import { useEffect, useState } from "react";
import { ArrowRight, Check, Fingerprint, Lock, Plus, ShieldCheck, ShieldX, X } from "lucide-react";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { cn } from "@shpyrd/ui/lib/cn";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { access } from "@shpyrd/content/site/tour";
import { Intro, Label, panel } from "./parts";

// What the app shows Marina: the claims waiting for her.
// How long each person has the app before the next.
const HOLD = 4200;

const claims = [
  { who: "Lucas Prado", what: "Team offsite, Lisbon", kind: "Travel", amount: "$1,240.00" },
  { who: "Bia Nunes", what: "Laptop stand", kind: "Equipment", amount: "$89.90" },
  { who: "Rafa Lima", what: "Client dinner", kind: "Meals", amount: "$312.50" },
];

export function Access() {
  const [who, setWho] = useState(0);
  const [moving, setMoving] = useState(true);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) setMoving(false);
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(() => setWho((w) => (w + 1) % access.people.length), HOLD);
    return () => window.clearTimeout(timer);
  }, [moving, who]);
  const person = access.people[who];

  return (
    <div className="grid gap-8">
      <Intro label={<Label icon={<Fingerprint />}>{access.label}</Label>} heading={access.heading} lead={access.lead} />

      <div className="grid gap-5 lg:grid-cols-[18rem_1fr]">
        <div className="grid content-start gap-2" role="radiogroup" aria-label="Who opens the app">
          {access.people.map((p, i) => (
            <button
              key={p.id}
              type="button"
              role="radio"
              aria-checked={i === who}
              onClick={() => {
                setWho(i);
                setMoving(false);
              }}
              className={cn(
                panel,
                "relative flex items-center gap-3 overflow-hidden px-3 py-2.5 text-left transition-colors duration-normal ease-move",
                i === who ? "border-primary/50 bg-primary/5" : "hover:border-foreground/20",
              )}
            >
              <Avatar alt={p.name} size={32} />
              <span className="grid min-w-0">
                <span className="font-medium">{p.name}</span>
                <span className="truncate text-xs text-muted-foreground">{p.email}</span>
              </span>
              {/* How long until the next person. */}
              {i === who && moving && (
                <span aria-hidden="true" className="absolute inset-y-2 right-2 w-1 overflow-hidden rounded-full bg-primary/15">
                  <span
                    key={who}
                    className="block w-full rounded-full bg-primary motion-safe:animate-[tour-fill_linear_forwards]"
                    style={{ animationDuration: `${HOLD}ms` }}
                  />
                </span>
              )}
            </button>
          ))}
          <p key={person.id} className={cn(panel, "mt-2 p-3 text-sm animate-in fade-in slide-in-from-bottom-2 zoom-in-[0.98] duration-slow ease-enter")}>
            <StatusBadge type={person.allowed ? "success" : "error"} className="mb-2">
              {person.allowed ? "Let in" : "Turned away"}
            </StatusBadge>
            <br />
            {person.verdict}
          </p>
        </div>

        <div className="grid gap-4">
          <BrowserFrame address={person.id === "outside" ? "https://acme.shpyrd.app/login" : access.url} className="min-h-72">
            <div key={person.id} className="grid min-h-64 animate-in fade-in slide-in-from-bottom-2 zoom-in-[0.98] duration-slow ease-enter blur-in-6">
              {person.allowed ? (
                // The customer's own app: whatever they built.
                <div className="grid content-start gap-4 p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="font-heading text-lg font-semibold">Expense claims</p>
                      <p className="text-xs text-muted-foreground">3 waiting for you · $1,642.40</p>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button size="sm" variant="outline" icon={<Plus />}>
                        New claim
                      </Button>
                      <Avatar alt="Marina Costa" size={28} />
                    </div>
                  </div>
                  <div className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                    {claims.map((c) => (
                      <div key={c.what} className="flex items-center gap-3 px-3 py-2.5 text-sm">
                        <Avatar alt={c.who} size={28} />
                        <div className="grid min-w-0 flex-1">
                          <span className="truncate font-medium">{c.what}</span>
                          <span className="text-xs text-muted-foreground">{c.who}</span>
                        </div>
                        <Badge variant="secondary" className="max-sm:hidden">{c.kind}</Badge>
                        <span className="w-20 text-right tabular-nums">{c.amount}</span>
                        <Button size="icon-sm" variant="outline" aria-label="Reject">
                          <X />
                        </Button>
                        <Button size="icon-sm" aria-label="Approve">
                          <Check />
                        </Button>
                      </div>
                    ))}
                  </div>
                </div>
              ) : person.id === "outside" ? (
                // shpyrd's sign-in, as the workspace draws it (apps/workspace,
                // screens/login.tsx), with only the company's provider offered.
                <div className="grid place-items-center px-6 py-10">
                  <div className="grid w-full max-w-xs justify-items-center gap-5 text-center">
                    <LogoMark className="size-12" />
                    <div className="grid gap-1">
                      <p className="font-heading text-xl font-medium">Acme</p>
                      <p className="text-sm text-muted-foreground">Sign in to open its applications.</p>
                    </div>
                    <Button size="xl" variant="outline" className="w-full" iconEnd={<ArrowRight />}>
                      Continue with Microsoft Entra
                    </Button>
                    <p className="text-xs text-muted-foreground">Only @yourcompany.com accounts can sign in.</p>
                  </div>
                </div>
              ) : (
                // shpyrd's own page for someone the app is not for
                // (design/pages, "no-access").
                <StatePage
                  icon={<Lock />}
                  title="Available to the finance team"
                  description="Ask a project admin to grant your team access, or sign in with an account that has it."
                  action={
                    <>
                      <Button variant="outline" size="lg">Sign in as someone else</Button>
                      <Button variant="outline" size="lg">Your apps</Button>
                    </>
                  }
                  className="min-h-72"
                />
              )}
            </div>
          </BrowserFrame>

          <div className={cn(panel, "grid gap-2 p-4")}>
            <p className="flex items-center gap-2 text-xs text-muted-foreground [&_svg]:size-3.5">
              {person.allowed ? <ShieldCheck className="text-success" /> : <ShieldX className="text-destructive" />}
              {access.headersLabel}
            </p>
            <pre key={person.id} className="overflow-x-auto font-mono text-xs leading-relaxed animate-in fade-in slide-in-from-bottom-2 zoom-in-[0.98] duration-slow ease-enter delay-100 fill-mode-both">
              {person.allowed
                ? `X-Shpyrd-User:   ${person.email}\nX-Shpyrd-Name:   Marina Costa\nX-Shpyrd-Teams:  finance,everyone\nX-Shpyrd-Roles:  user`
                : "Nothing: the request never reached the app."}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
}
