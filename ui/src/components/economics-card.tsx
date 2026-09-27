import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { TrendingUp } from "lucide-react";

import { api } from "@/lib/api";
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

/**
 * Operator economics (RFC-0075): revenue, COGS from OpenCost, and gross
 * margin per workspace. Operator-only — never shown to customers.
 */
export function EconomicsCard() {
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
            Install the opencost extension to see cost data (shpyrd-ctl
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
                      {r.revenue.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {r.totalCogs.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {r.grossMargin.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {r.marginPct.toFixed(1)}%
                    </TableCell>
                  </TableRow>
                ))}
                {econ.data.totals && (
                  <TableRow className="font-semibold">
                    <TableCell>Total</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {econ.data.totals.revenue.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {econ.data.totals.totalCogs.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {econ.data.totals.grossMargin.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {econ.data.totals.marginPct.toFixed(1)}%
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
