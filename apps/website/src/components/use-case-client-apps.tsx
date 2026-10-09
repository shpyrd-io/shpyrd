import { Button } from "@shpyrd/ui/components/button";
import { contact } from "@shpyrd/content/site/offer";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";
import { SimpleCta } from "@/components/simple-cta";

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
    <SimpleCta
      heading={heading}
      description={description}
      action={
        <Button size="lg" asChild>
          <a href={signUp}>Get started</a>
        </Button>
      }
      links={[
        { label: "Talk to us", href: contact.href },
        { label: "For FDE partners", href: "/for/fde-partners" },
      ]}
    />
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
