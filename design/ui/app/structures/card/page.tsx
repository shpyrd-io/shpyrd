"use client";

import { Earth, ExternalLink, Lock } from "lucide-react";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Overview</CardTitle>
              <CardDescription>What a card of the dashboard looks like.</CardDescription>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">
              Release v12, two instances ready.
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Loading</CardTitle>
              <CardDescription>The same card while it waits.</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-2">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </CardContent>
          </Card>
        </div>
      </Section>
      <Section title="Secondary, with a shadow">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Card variant="secondary">
            <CardHeader>
              <CardTitle>Secondary</CardTitle>
              <CardDescription>The card with a shadow, lifted from the page.</CardDescription>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">
              Release v12, two instances ready.
            </CardContent>
          </Card>
          <Card variant="secondary">
            <CardHeader>
              <CardTitle>Secondary, loading</CardTitle>
              <CardDescription>The same card while it waits.</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-2">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </CardContent>
          </Card>
        </div>
      </Section>
      <Section title="A card that is a link answers to the pointer and to the keyboard">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Card asChild>
            <a href="#docs001" onClick={(e) => e.preventDefault()}>
              <CardHeader>
                <CardTitle>Docs 001</CardTitle>
                <CardDescription className="font-mono text-xs">
                  docs001.platform.shpyrd.app
                </CardDescription>
                <CardAction>
                  <Earth className="size-4 text-muted-foreground" aria-label="public" />
                </CardAction>
              </CardHeader>
              <CardContent className="flex items-center justify-between">
                <StatusBadge type="success">Running</StatusBadge>
                {open}
              </CardContent>
            </a>
          </Card>
          <Card asChild variant="secondary">
            <a href="#hello" onClick={(e) => e.preventDefault()}>
              <CardHeader>
                <CardTitle>hello</CardTitle>
                <CardDescription className="font-mono text-xs">
                  hello.platform.shpyrd.app
                </CardDescription>
                <CardAction>
                  <Lock
                    className="size-4 text-muted-foreground"
                    aria-label="sign-in required"
                  />
                </CardAction>
              </CardHeader>
              <CardContent className="flex items-center justify-between">
                <StatusBadge type="success">Running</StatusBadge>
                {open}
              </CardContent>
            </a>
          </Card>
        </div>
      </Section>
    </>
  );
}

const open = (
  <span className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors group-hover/card:text-foreground group-focus-visible/card:text-foreground">
    Open <ExternalLink className="size-3" />
  </span>
);
