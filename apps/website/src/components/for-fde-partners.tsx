import { Button } from "@shpyrd/ui/components/button";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact, developerCta } from "@shpyrd/content/site/offer";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";
import { SimpleCta } from "@/components/simple-cta";

// What the pages of "For FDE partners" share: the full ending and the limits
// (on the section's own page), and the short step at the end of each subpage.
// shpyrd cloud comes first: a partner signs up and starts a client's workspace
// there; the client's own cloud is an option, on its own subpage. The research
// is explicit that there is no partner programme, fleet management, per-client
// billing or SLA to offer, so none is.
//

export function PartnerNext({ heading = "Start with one client" }: { heading?: string }) {
  return (
    <SimpleCta
      heading={heading}
      description="Sign up, open a workspace for the client you are delivering to now, and deploy what you built. The next client gets the same setup."
      action={
        <Button size="lg" asChild>
          <a href={signUp}>Get started</a>
        </Button>
      }
      links={[
        { label: "Talk to us about a client project", href: contact.href },
        { label: "Quick start for your engineers", href: developerCta.href },
      ]}
    />
  );
}

// The end of a subpage: the next tab, and the one thing to do.
export function PartnerStep({
  next,
  action = { label: "Get started", href: signUp },
}: {
  next: { href: string; title: string };
  action?: { label: string; href: string };
}) {
  return (
    <SubpageStep
      action={
        <Button asChild>
          <a href={action.href}>{action.label}</a>
        </Button>
      }
      next={next}
    />
  );
}

const limits = [
  {
    claim: "One workspace per client.",
    limit:
      "Each client has its own workspace, on shpyrd cloud or in their own cloud. There is no console across all your clients, and no billing per client.",
  },
  {
    claim: "No SLA today.",
    limit:
      "shpyrd cloud has no service-level agreement yet. In the client's own cloud, the cluster is run by you or by the client's team.",
  },
  {
    claim: "The app's own rules stay in the app.",
    limit:
      "shpyrd decides who can open it. What a person may do inside it is the app's job, and yours to build.",
  },
];

export function PartnerLimits() {
  return (
    <Stack gap="spacious">
      <SectionIntro align="center" variant="xlarge" heading="What it doesn't do" />
      {/* The three limits side by side, as on the developers' and IT pages;
          one under the other on a phone. */}
      <ul className="grid gap-8 md:grid-cols-3">
        {limits.map((l) => (
          <li key={l.claim} className="border-l-2 pl-4">
            <p className="font-semibold">{l.claim}</p>
            <p className="mt-1 max-w-prose text-muted-foreground">{l.limit}</p>
          </li>
        ))}
      </ul>
    </Stack>
  );
}
