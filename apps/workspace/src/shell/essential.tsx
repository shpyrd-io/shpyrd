"use client";

import { Link } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import { Card, CardContent } from "@shpyrd/ui/components/card";

// One of the few things people change, with how it is now: a tile that
// opens its page. Owed to design/ui once it settles.
export function Essential({ to, icon: Icon, title, now }: { to: string; icon: LucideIcon; title: string; now: string }) {
  return (
    <Card size="sm" asChild>
      <Link to={to}>
        <CardContent className="grid gap-2">
          <Icon className="size-4 text-muted-foreground" />
          <div>
            <div className="font-medium">{title}</div>
            <div className="truncate text-muted-foreground">{now}</div>
          </div>
        </CardContent>
      </Link>
    </Card>
  );
}

// A row of them: two across, four when there is room.
export function Essentials({ children }: { children: React.ReactNode }) {
  return <div className="grid gap-4 @xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-4">{children}</div>;
}
