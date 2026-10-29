// Slide 11: let everyone build, we run the rest. In the new home's close
// (2026-10-20): the orange band with the yard in the logo's bar, and under
// it the 30-day pilot as the home's road, a step a week, drawn as it opens.
import { BarChart3, Check, FolderInput, KeyRound, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { CtaBand } from "@shpyrd/ui/components/cta-band";
import { closing } from "@/content/tour-v3";
import { enterpriseContact } from "@/content/offer";
import { DeployButton } from "@/components/deploy-button";
import { RouteTimeline } from "@/components/route-timeline";
import { ShipyardBar } from "@/components/shipyard-bar";

const weekIcons: Record<string, React.ReactElement> = {
  "week-1": <KeyRound />,
  "week-2": <FolderInput />,
  "week-3": <Users />,
  "week-4": <BarChart3 />,
};

export function Closing() {
  return (
    <div className="grid gap-10">
      <CtaBand
        bleed={false}
        // The bar white on the orange, the words with less room above and
        // under them than on a page and a wider column, so the band is
        // shorter and the pilot fits the slide too.
        className="[&_[data-slot=hero-image]]:mix-blend-normal! [&_.cta-band-hero>div>div:first-child]:pt-[26px]! [&_.cta-band-hero>div>div:first-child]:pb-12! @3xl/hero:[&_.cta-band-hero>div]:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]!"
        heading={
          <>
            {closing.heading[0]}
            <br />
            {closing.heading[1]}
          </>
        }
        description={closing.lead}
        actions={
          <>
            <DeployButton size="lg" />
            <Button asChild variant="link" size="lg">
              <a href={enterpriseContact.href}>{closing.contact}</a>
            </Button>
          </>
        }
        note={
          <span className="flex flex-wrap gap-x-4 gap-y-1">
            {closing.points.map((p) => (
              <span
                key={p}
                className="inline-flex items-center gap-1 [&_svg]:size-3.5"
              >
                <Check />
                {p}
              </span>
            ))}
          </span>
        }
        image={<ShipyardBar />}
      />

      {/* The pilot, a step a week, as the home's road. */}
      <div className="grid gap-6">
        <p className="text-center text-sm font-medium tracking-wide text-primary uppercase">
          {closing.pilot.title}
        </p>
        <RouteTimeline
          steps={closing.pilot.weeks.map((w, i) => ({
            id: `week-${i + 1}`,
            title: `Week ${i + 1} · ${w.title}`,
            body: w.body,
          }))}
          icons={weekIcons}
        />
      </div>
    </div>
  );
}
