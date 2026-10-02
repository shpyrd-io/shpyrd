import { Button } from "@shpyrd/ui/components/button";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact } from "@shpyrd/content/site/offer";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";

// The endings of the Client apps section. The section is about the app built
// for a client; the firm that builds it has its own page (/for/fde-partners).
// The section's own page ends with the whole of it; each subpage ends with a
// short step to the next one.

export function ClientAppsNext({
  heading = "Building one now?",
  description = "Bring the app you are delivering. We look at where the client needs it to run and whose sign-in it needs, and you decide whether it goes this way.",
}: {
  heading?: string;
  description?: string;
}) {
  return (
    <Stack gap="normal" className="border-t pt-12">
      <SectionIntro heading={heading} description={description} />
      <Stack direction="horizontal" gap="cozy" className="flex-wrap">
        {/* Self sign-up on shpyrd cloud. */}
        <Button asChild>
          <a href={signUp}>Get started</a>
        </Button>
        <Button variant="outline" asChild>
          <a href={contact.href}>Talk to us</a>
        </Button>
        <Button variant="link" asChild className="px-0">
          <a href="/for/fde-partners">For FDE partners</a>
        </Button>
      </Stack>
    </Stack>
  );
}

// The end of a subpage: the next one in the section, and the one thing to do.
export function ClientAppsStep({ next }: { next: { href: string; label: string } }) {
  return (
    <SubpageStep
      action={
        <Button asChild>
          <a href={contact.href}>Talk to us</a>
        </Button>
      }
      next={{ href: next.href, title: next.label }}
    />
  );
}
