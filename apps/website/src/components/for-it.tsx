import { Button } from "@shpyrd/ui/components/button";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact } from "@shpyrd/content/site/offer";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";

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
    <Stack gap="spacious" className="border-t pt-12">
      <SectionIntro align="center" variant="xlarge" heading={heading} description={description} />
      <Stack direction="horizontal" gap="cozy" className="flex-wrap">
        <Button asChild>
          <a href={getStarted.href}>{getStarted.label}</a>
        </Button>
        <Button variant="outline" asChild>
          <a href={contact.href}>Talk to us about a pilot</a>
        </Button>
        <Button variant="ghost" asChild>
          <a href="/docs/access">People, teams and roles</a>
        </Button>
      </Stack>
    </Stack>
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
