"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Input } from "@shpyrd/ui/components/input";
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { EconomicsRow } from "@/api/types";
import { Failed, Loading } from "./shared";

const money = (n: number | undefined | null, digits = 2) => (typeof n === "number" && Number.isFinite(n) ? n : 0).toFixed(digits);

const sumOf = (rows: EconomicsRow[], key: keyof EconomicsRow) => rows.reduce((a, r) => a + ((r[key] as number) || 0), 0);

// What the platform earns and what it costs, by workspace: revenue at
// the plan's prices, the infrastructure's cost from OpenCost, and the
// margin. The operator's own workspaces are expenses, never revenue.
export function Economics() {
  const [month, setMonth] = useState("");
  const econ = useQuery({ queryKey: ["economics", month], queryFn: () => api.economics(month || undefined), staleTime: 300_000, retry: false });
  const rows = econ.data?.workspaces ?? [];
  const customers = rows.filter((r) => r.owner !== "operator");
  const own = rows.filter((r) => r.owner === "operator");
  const revenue = sumOf(customers, "revenue");
  const cogs = sumOf(customers, "totalCogs");
  const empty = econ.data && (econ.data.totals?.totalCogs ?? 0) === 0 && (econ.data.totals?.revenue ?? 0) === 0;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Customers</CardTitle>
          <CardDescription>Revenue at the prices of the plan, the cost of the infrastructure, and what is left. Direct is the pods in the workspace's namespaces; shared its part of the ingress, the monitoring and the platform; idle its part of the capacity nobody uses.</CardDescription>
          <CardAction>
            <Input type="month" size="sm" value={month} onChange={(e) => setMonth(e.target.value)} aria-label="Month" className="w-40" />
          </CardAction>
        </CardHeader>
        <CardContent>
          {econ.isLoading ? (
            <Loading />
          ) : econ.error ? (
            <Failed what="the economics" error={econ.error} />
          ) : empty ? (
            <p className="text-sm text-muted-foreground">Nothing recorded for {econ.data?.month} yet. Revenue appears once a workspace has a plan; the cost once the opencost extension is on.</p>
          ) : customers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No customer workspace yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workspace</TableHead>
                  <TableHead className="text-right">Revenue</TableHead>
                  <TableHead className="text-right">Direct</TableHead>
                  <TableHead className="text-right">Shared</TableHead>
                  <TableHead className="text-right">Idle</TableHead>
                  <TableHead className="text-right">Cost</TableHead>
                  <TableHead className="text-right">Margin</TableHead>
                  <TableHead className="text-right">Margin %</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {customers.map((r) => (
                  <TableRow key={r.workspace}>
                    <TableCell className="font-medium">{r.workspace}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{money(r.revenue)}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.directCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.sharedCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.idleCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{money(r.totalCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{money(r.grossMargin)}</TableCell>
                    <TableCell className={`text-right font-mono text-xs ${r.marginPct < 20 ? "text-destructive" : ""}`}>{money(r.marginPct, 1)}%</TableCell>
                  </TableRow>
                ))}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell>{econ.data?.month}, the customers</TableCell>
                  <TableCell className="text-right font-mono">{money(revenue)}</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(customers, "directCogs"))}</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(customers, "sharedCogs"))}</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(customers, "idleCogs"))}</TableCell>
                  <TableCell className="text-right font-mono">{money(cogs)}</TableCell>
                  <TableCell className="text-right font-mono">{money(revenue - cogs)}</TableCell>
                  <TableCell className="text-right font-mono">{money(revenue > 0 ? ((revenue - cogs) / revenue) * 100 : 0, 1)}%</TableCell>
                </TableRow>
              </TableFooter>
            </Table>
          )}
        </CardContent>
      </Card>
      {own.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Operating expenses</CardTitle>
            <CardDescription>The operator's own workspaces: their costs are the platform's, and they are never invoiced.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workspace</TableHead>
                  <TableHead className="text-right">Direct</TableHead>
                  <TableHead className="text-right">Shared</TableHead>
                  <TableHead className="text-right">Idle</TableHead>
                  <TableHead className="text-right">Cost</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {own.map((r) => (
                  <TableRow key={r.workspace}>
                    <TableCell className="font-medium">{r.workspace}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.directCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.sharedCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{money(r.idleCogs)}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{money(r.totalCogs)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell>{econ.data?.month}, the platform's own</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(own, "directCogs"))}</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(own, "sharedCogs"))}</TableCell>
                  <TableCell className="text-right font-mono text-muted-foreground">{money(sumOf(own, "idleCogs"))}</TableCell>
                  <TableCell className="text-right font-mono">{money(sumOf(own, "totalCogs"))}</TableCell>
                </TableRow>
              </TableFooter>
            </Table>
          </CardContent>
        </Card>
      )}
    </>
  );
}
