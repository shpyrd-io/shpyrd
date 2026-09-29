import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { TrendingUp } from "lucide-react";

import { api, type EconomicsRow } from "@/lib/api";
import { ErrorBoundary } from "@/components/error-boundary";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/** money formats an amount that may be missing from an older server. */
function money(n: number | undefined | null, digits = 2): string {
  return (typeof n === "number" && Number.isFinite(n) ? n : 0).toFixed(digits);
}

/**
 * Operator economics (RFC-0075): revenue, COGS from OpenCost, and gross
 * margin per workspace. Operator-only — never shown to customers.
 */
export function EconomicsCard() {
  return (
    <ErrorBoundary what="The Economics card">
      <EconomicsCardInner />
    </ErrorBoundary>
  );
}

function EconomicsCardInner() {
  const [month, setMonth] = useState("");
  const econ = useQuery({
    queryKey: ["economics", month],
    queryFn: () => api.economics(month || undefined),
    staleTime: 300_000,
    retry: false,
  });

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="size-4" /> Economics
            </CardTitle>
            <CardDescription>
              Revenue at plan prices, infrastructure COGS from OpenCost, and
              gross margin per workspace. Operator-only — never shown to
              customers.
            </CardDescription>
          </div>
          <input
            type="month"
            className="rounded border px-2 py-1 text-xs"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
          />
        </div>
      </CardHeader>
      <CardContent>
        {econ.isLoading && <Skeleton className="h-24 w-full" />}
        {econ.error && (
          <p className="text-sm text-muted-foreground">
            Economics could not be loaded: {(econ.error as Error).message}
          </p>
        )}
        {econ.data &&
          (econ.data.totals?.totalCogs ?? 0) === 0 &&
          (econ.data.totals?.revenue ?? 0) === 0 && (
            <p className="mb-3 text-sm text-muted-foreground">
              No revenue or cost recorded for this month yet. Revenue appears
              once a workspace has a plan (shpyrd-ctl plans assign); COGS
              appears once the opencost extension is enabled (shpyrd-ctl
              extensions enable opencost).
            </p>
          )}
        {econ.data && (
          <>
            <EconomicsTable
              title="Customers"
              rows={(econ.data.workspaces ?? []).filter(
                (r) => r.owner !== "operator",
              )}
              totals={customerTotals(econ.data.workspaces ?? [])}
              empty="No customer workspace yet."
            />
            <EconomicsTable
              title="Operating expenses"
              hint="The operator's own workspaces (RFC-0078): their costs are the platform's, they are never invoiced."
              rows={(econ.data.workspaces ?? []).filter(
                (r) => r.owner === "operator",
              )}
              totals={operatorTotals(econ.data.workspaces ?? [])}
              expenses
              empty="No operator workspace."
            />
            <p className="mt-2 text-xs text-muted-foreground">
              Period: {econ.data.month}. Revenue = plan prices × usage. Direct =
              pods in the workspace's namespaces. Shared = proportional share of
              ingress, monitoring and platform infrastructure. Idle =
              proportional share of unused node capacity (shrinks when
              autoscaling removes nodes).
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

/** Sums of the customer rows: revenue, COGS and margin. */
function customerTotals(rows: EconomicsRow[]): EconomicsRow | undefined {
  const c = rows.filter((r) => r.owner !== "operator");
  if (c.length === 0) return undefined;
  const sum = (k: keyof EconomicsRow) =>
    c.reduce((a, r) => a + ((r[k] as number) || 0), 0);
  const revenue = sum("revenue");
  const totalCogs = sum("totalCogs");
  return {
    workspace: "Customer totals",
    revenue,
    directCogs: sum("directCogs"),
    sharedCogs: sum("sharedCogs"),
    idleCogs: sum("idleCogs"),
    totalCogs,
    grossMargin: revenue - totalCogs,
    marginPct: revenue > 0 ? ((revenue - totalCogs) / revenue) * 100 : 0,
  };
}

/** Sums of the operator rows: costs only. */
function operatorTotals(rows: EconomicsRow[]): EconomicsRow | undefined {
  const o = rows.filter((r) => r.owner === "operator");
  if (o.length === 0) return undefined;
  const sum = (k: keyof EconomicsRow) =>
    o.reduce((a, r) => a + ((r[k] as number) || 0), 0);
  return {
    workspace: "Operating expenses",
    revenue: 0,
    directCogs: sum("directCogs"),
    sharedCogs: sum("sharedCogs"),
    idleCogs: sum("idleCogs"),
    totalCogs: sum("totalCogs"),
    grossMargin: 0,
    marginPct: 0,
  };
}

/**
 * One economics table: customers carry revenue and margin; the operator's
 * workspaces are expenses and show costs only (the CLI's
 * `shpyrd-ctl economics` draws the same two blocks).
 */
function EconomicsTable({
  title,
  hint,
  rows,
  totals,
  expenses = false,
  empty,
}: {
  title: string;
  hint?: string;
  rows: EconomicsRow[];
  totals?: EconomicsRow;
  expenses?: boolean;
  empty: string;
}) {
  return (
    <div className="mt-4 first:mt-0">
      <div className="mb-2">
        <div className="text-sm font-medium">{title}</div>
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </div>
      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">{empty}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Workspace</TableHead>
              {!expenses && (
                <TableHead className="text-right">Revenue</TableHead>
              )}
              <TableHead className="text-right">Direct</TableHead>
              <TableHead className="text-right">Shared</TableHead>
              <TableHead className="text-right">Idle</TableHead>
              <TableHead className="text-right">
                {expenses ? "Total cost" : "Total COGS"}
              </TableHead>
              {!expenses && (
                <TableHead className="text-right">Margin</TableHead>
              )}
              {!expenses && (
                <TableHead className="text-right">Margin %</TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <EconomicsRowView key={r.workspace} r={r} expenses={expenses} />
            ))}
            {totals && (
              <EconomicsRowView r={totals} expenses={expenses} total />
            )}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function EconomicsRowView({
  r,
  expenses,
  total = false,
}: {
  r: EconomicsRow;
  expenses: boolean;
  total?: boolean;
}) {
  const cell = "text-right font-mono text-xs";
  return (
    <TableRow className={total ? "font-semibold" : undefined}>
      <TableCell className={total ? "" : "font-mono text-xs"}>
        {r.workspace}
      </TableCell>
      {!expenses && <TableCell className={cell}>{money(r.revenue)}</TableCell>}
      <TableCell className={cell + " text-muted-foreground"}>
        {money(r.directCogs)}
      </TableCell>
      <TableCell className={cell + " text-muted-foreground"}>
        {money(r.sharedCogs)}
      </TableCell>
      <TableCell className={cell + " text-muted-foreground"}>
        {money(r.idleCogs)}
      </TableCell>
      <TableCell className={cell}>{money(r.totalCogs)}</TableCell>
      {!expenses && (
        <TableCell className={cell}>{money(r.grossMargin)}</TableCell>
      )}
      {!expenses && (
        <TableCell className={cell}>{money(r.marginPct, 1)}%</TableCell>
      )}
    </TableRow>
  );
}
