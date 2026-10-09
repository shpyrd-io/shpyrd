import { Button } from "@shpyrd/ui/components/button";
import { contact } from "@shpyrd/content/site/offer";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";
import { SimpleCta } from "@/components/simple-cta";

// Where "Get started" goes: signing up for a workspace on shpyrd cloud.
export const getStarted = { label: "Get started", href: signUp };

// The end of the "For IT teams" section's own page. Start on shpyrd cloud;
// or bring one app and one team to a pilot; or read how access works.
export function ItNext({
  heading = "Start with one app and one team",
  description = "Sign up for a workspace on shpyrd cloud, connect your company's sign-in, and give one team its first app. Or talk to us about a pilot first.",
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
          <a href={getStarted.href}>{getStarted.label}</a>
        </Button>
      }
      links={[
        { label: "Talk to us about a pilot", href: contact.href },
        { label: "People, teams and roles", href: "/docs/access" },
      ]}
    />
  );
}

// The end of a subpage: the next tab of the section, and one thing to do.
// The full set of next steps is on the section's own page.
export function ItNextStep({
  next,
  action = getStarted,
}: {
  next: { label: string; href: string };
  action?: { label: string; href: string };
}) {
  return (
    <SubpageStep
      action={
        <Button asChild>
          <a href={action.href}>{action.label}</a>
        </Button>
      }
      next={{ href: next.href, title: next.label }}
    />
  );
}
