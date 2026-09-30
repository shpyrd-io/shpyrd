import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";

// The end of a subpage of a solution or a use case: what to do, on the left,
// and the next subpage, on the right. Short on purpose: the section's own page
// has the full ending; a subpage only says where to go from here.
export function SubpageStep({
  action,
  next,
}: {
  action?: React.ReactNode;
  next?: { href: string; title: string };
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3 border-t pt-8">
      {action}
      {next && (
        <Button variant="ghost" asChild iconEnd={<ArrowRight />} className="ml-auto">
          <Link href={next.href}>Next: {next.title}</Link>
        </Button>
      )}
    </div>
  );
}
