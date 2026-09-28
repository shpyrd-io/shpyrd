import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { TrendingUp } from "lucide-react";

import { api } from "@/lib/api";
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
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workspace</TableHead>
                  <TableHead className="text-right">Revenue</TableHead>
                  <TableHead className="text-right">COGS</TableHead>
                  <TableHead className="text-right">Margin</TableHead>
                  <TableHead className="text-right">Margin %</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {econ.data.workspaces?.map((r) => (
                  <TableRow key={r.workspace}>
                    <TableCell className="font-mono text-xs">
                      {r.workspace}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(r.revenue)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(r.totalCogs)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(r.grossMargin)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(r.marginPct, 1)}%
                    </TableCell>
                  </TableRow>
                ))}
                {econ.data.totals && (
                  <TableRow className="font-semibold">
                    <TableCell>Total</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(econ.data.totals.revenue)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(econ.data.totals.totalCogs)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(econ.data.totals.grossMargin)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {money(econ.data.totals.marginPct, 1)}%
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
            <p className="mt-2 text-xs text-muted-foreground">
              Period: {econ.data.month}. Revenue = plan prices × usage
              (estimate). COGS = OpenCost allocation including idle and shared
              infra.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
