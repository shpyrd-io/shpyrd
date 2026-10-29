"use client";

// Slide 6's browser: what each person sees there (the slide itself is in
// access-scenarios.tsx).
import { Building2, Check, Lock, Plus, X } from "lucide-react";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { PersonMark } from "./person-mark";
import { access } from "@/content/tour-v3";

// What the app shows Marina: the claims waiting for her.
const claims = [
  {
    who: "Lucas Prado",
    what: "Team offsite, Lisbon",
    kind: "Travel",
    amount: "$1,240.00",
  },
  {
    who: "Bia Nunes",
    what: "Laptop stand",
    kind: "Equipment",
    amount: "$89.90",
  },
  { who: "Rafa Lima", what: "Client dinner", kind: "Meals", amount: "$312.50" },
];

// What each person's browser shows: the app, shpyrd's sign-in, or its page
// for someone the app is not for. Shared with the scenarios test.
export function AccessView({
  person,
}: {
  person: (typeof access.people)[number];
}) {
  return (
    <>
      {person.allowed ? (
        // The customer's own app: whatever they built.
        <div className="grid content-start gap-4 p-5 max-sm:p-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="font-heading text-lg font-semibold whitespace-nowrap">
                Expense claims
              </p>
              <p className="text-xs text-muted-foreground">
                3 waiting for you · $1,642.40
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                icon={<Plus />}
                className="max-sm:hidden"
              >
                New claim
              </Button>
              <PersonMark look="woman" className="size-7" />
            </div>
          </div>
          <div className="divide-y divide-border overflow-hidden rounded-2xl border border-border">
            {claims.map((c) => (
              <div
                key={c.what}
                className="flex items-center gap-3 px-3 py-2.5 text-sm"
              >
                <Avatar alt={c.who} size={28} className="max-sm:hidden" />
                <div className="grid min-w-0 flex-1">
                  <span className="truncate font-medium">{c.what}</span>
                  <span className="text-xs text-muted-foreground">{c.who}</span>
                </div>
                <Badge variant="secondary" className="max-sm:hidden">
                  {c.kind}
                </Badge>
                <span className="text-right tabular-nums sm:w-20">{c.amount}</span>
                <Button
                  size="icon-sm"
                  variant="outline"
                  aria-label="Reject"
                  className="max-sm:hidden"
                >
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
              <p className="font-heading text-xl font-medium">Your company</p>
              <p className="text-sm text-muted-foreground">
                Sign in to open its applications.
              </p>
            </div>
            {/* As slide 4's sign-in window (Giovani, 2026-10-28). */}
            <Button size="xl" className="w-full" icon={<Building2 />}>
              Company sign-in
            </Button>
            <p className="text-xs text-muted-foreground">
              Only @yourcompany.com accounts can sign in.
            </p>
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
              <Button variant="outline" size="lg">
                Sign in as someone else
              </Button>
              <Button variant="outline" size="lg">
                Your apps
              </Button>
            </>
          }
          className="min-h-72"
        />
      )}
    </>
  );
}
