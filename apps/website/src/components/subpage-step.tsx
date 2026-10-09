import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";

// The end of a page of a solution or a use case, its overview included: what
// to do, on the left, and the next subpage, on the right, as an outline
// button. The button always stands 48px over the footer's line, at the foot
// of its row, whatever the action beside it holds under itself. Short on purpose: the section's own page
// has the full ending; a subpage only says where to go from here.
export function SubpageStep({
  action,
  next,
}: {
  action?: React.ReactNode;
  next?: { href: string; title: string };
}) {
  return (
    // No line of its own: it stands the site's 168px under the section
    // above, as any section does, and 48px over the footer's line (its 24px
    // and the page's own; pageSections gives it none of its 144px).
    <div
      data-slot="next-step"
      className="flex flex-wrap items-start gap-x-6 gap-y-3 pb-6"
    >
      {action}
      {next && (
        <Button variant="outline" asChild iconEnd={<ArrowRight />} className="ml-auto self-end">
          <Link href={next.href}>Next: {next.title}</Link>
        </Button>
      )}
    </div>
  );
}
