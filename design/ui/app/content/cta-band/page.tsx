import { Button } from "@shpyrd/ui/components/button";
import { CtaBand } from "@shpyrd/ui/components/cta-band";
import { Section } from "../../section";

export default function Page() {
  return (
    <Section title="Within its box here; on a page of the site, from edge to edge of the window (bleed), before the footer">
      <div className="py-6">
        <CtaBand
          bleed={false}
          heading="Start on shpyrd cloud"
          description="Sign up, connect your agent, and ship your first app. Or run it yourself: shpyrd is open source under MPL-2.0, and the quick start takes you from nothing to a deployed app."
          actions={
            <>
              <Button size="lg" asChild>
                <a href="#">Get started</a>
              </Button>
              <Button variant="link" size="lg" asChild>
                <a href="#">Run it yourself</a>
              </Button>
            </>
          }
        />
      </div>
    </Section>
  );
}
