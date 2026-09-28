import { useQuery } from "@tanstack/react-query";
import { ReceiptText } from "lucide-react";

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

/**
 * The workspace's month-to-date billing estimate (RFC-0075). Shown on the
 * Workspace > Overview tab for workspace admins. No money changes hands yet —
 * this is a preview at plan prices.
 */
export function BillingCard() {
  return (
    <ErrorBoundary what="The Billing card">
      <BillingCardInner />
    </ErrorBoundary>
  );
}

/** num guards amounts that may be missing from an older server. */
function num(n: number | undefined | null): number {
  return typeof n === "number" && Number.isFinite(n) ? n : 0;
}

function BillingCardInner() {
  const billing = useQuery({
    queryKey: ["billing-current"],
    queryFn: api.billingCurrent,
    staleTime: 60_000,
    retry: false,
  });

  if (billing.error) return null; // no plan: card is absent
  if (billing.isLoading) return <Skeleton className="h-32 w-full" />;
  if (!billing.data) return null;

  const b = billing.data;
  const currency = b.currency || "USD";
  const planName = b.plan?.name ?? "no plan";
  const partial = b.quality !== "complete";

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ReceiptText className="size-4" /> Billing
        </CardTitle>
        <CardDescription>
          Month-to-date estimate at plan prices ({planName}
          {partial ? ", some gaps in the data" : ""}). No money is owed until a
          payment provider is connected.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {(b.lines ?? []).length === 0 && (
          <p className="text-sm text-muted-foreground">
            No usage recorded yet.
          </p>
        )}
        {(b.lines ?? []).length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Component</TableHead>
                <TableHead>Metric</TableHead>
                <TableHead className="text-right">Quantity</TableHead>
                <TableHead className="text-right">Unit price</TableHead>
                <TableHead className="text-right">Amount</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(b.lines ?? []).map((l) => (
                <TableRow key={l.component + l.metric}>
                  <TableCell className="font-mono text-xs">
                    {l.component}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {l.metric.replace(/_/g, " ")}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {num(l.quantity).toFixed(4)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {num(l.unitPrice) > 0 ? num(l.unitPrice).toFixed(6) : "—"}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {l.grossAmount > 0
                      ? `${currency} ${num(l.grossAmount).toFixed(4)}`
                      : "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span className="text-muted-foreground">
            Projected month total:{" "}
            <strong>
              {currency} {num(b.projection).toFixed(2)}
            </strong>
          </span>
          <span className="text-muted-foreground">
            This month so far:{" "}
            <strong>
              {currency} {num(b.total).toFixed(4)}
            </strong>
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
