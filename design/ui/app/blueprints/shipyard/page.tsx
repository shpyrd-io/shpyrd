import { Shipyard, shipyardSprites } from "@shpyrd/ui/components/shipyard";
import { Button } from "@shpyrd/ui/components/button";
import { Section } from "../../section";

// A shipyard at work, for the first page of the site: the picture that
// was still, moving.
export default function Page() {
  return (
    <>
      <Section title="Beside the words of a first page, and under them where there is no room">
        <div className="grid items-center gap-8 @3xl/page-layout:grid-cols-2">
          <div className="grid gap-4">
            <h3 className="text-4xl font-semibold tracking-tight">One place to share apps with your team.</h3>
            <p className="text-muted-foreground">
              Bring the apps your team builds, choose who can use or manage them, and give colleagues one place to
              find them.
            </p>
            <div>
              <Button>See how sharing works</Button>
            </div>
          </div>
          <Shipyard />
        </div>
      </Section>
      <Section title="Standing still">
        <Shipyard paused seed={3} className="max-w-md" />
      </Section>
      <Section title="The pieces: a file for each, to be drawn again">
        <div className="grid grid-cols-2 gap-4 @3xl/page-layout:grid-cols-4">
          {Object.entries(shipyardSprites)
            .filter(([name]) => name !== "ground")
            .map(([name, sprite]) => (
              <div key={name} className="grid gap-2">
                <div className="grid h-40 place-items-center rounded-lg p-3 ring-1 ring-foreground/10">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img src={sprite.src} alt="" className="max-h-full max-w-full dark:hue-rotate-180 dark:invert" />
                </div>
                <span className="font-mono text-xs text-muted-foreground">{name}.svg</span>
              </div>
            ))}
        </div>
      </Section>
    </>
  );
}
