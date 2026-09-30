import { Button } from "@shpyrd/ui/components/button";
import { contact, secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { ShipyardCta } from "@/components/shipyard-cta";
import { SubpageStep } from "@/components/subpage-step";

// What the pages of "Apps from a hackathon" share. The overview ends with the
// full call to action and the shipyard beside it; the subpages end with a
// short step: at most one action, and the way to the next subpage.

export function HackathonNext({
  heading = "Start with the one people asked for",
  description = "Connect your agent once. Then sharing an app is a sentence.",
}: {
  heading?: string;
  description?: string;
}) {
  return (
    <ShipyardCta
      heading={heading}
      description={description}
      actions={
        <>
          <AddToAgent />
          <Button variant="outline" asChild>
            <a href={secondaryCta.href}>{secondaryCta.label}</a>
          </Button>
        </>
      }
      note={
        <>
          Ran the hackathon?{" "}
          <a href={contact.href} className="underline underline-offset-4 hover:text-foreground">
            Talk to us about the apps worth keeping
          </a>
          .
        </>
      }
    />
  );
}

// The end of a subpage: one action, if it has one, and the next subpage.
export function HackathonStep({
  next,
  action,
}: {
  next: { title: string; href: string };
  action?: React.ReactNode;
}) {
  return <SubpageStep action={action} next={next} />;
}
