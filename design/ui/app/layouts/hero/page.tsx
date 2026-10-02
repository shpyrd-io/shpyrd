"use client";

import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { Badge } from "@shpyrd/ui/components/badge";
import { Section } from "../../section";

// The headings here are inside a page that has its own, so they are `h2`:
// a page has one `h1`.
const stay = (event: React.MouseEvent) => event.preventDefault();

// A stand-in for the picture a hero carries, drawn rather than fetched: the
// gallery is built and drawn again in the browser, and both have to draw the
// same thing.
function Placeholder() {
  return (
    <div
      aria-hidden="true"
      className="grid aspect-[4/3] place-items-center rounded-xl border bg-muted text-sm text-muted-foreground"
    >
      a picture
    </div>
  );
}

export default function Page() {
  return (
    <>
      <Section title="A heading and what to do about it">
        <Hero
          as="h2"
          heading="One place to share apps with your team."
          description="Bring the apps your team builds, choose who can use or manage them, and give colleagues one place to find them."
          actions={
            <>
              <Button onClick={stay}>Add to Claude Code</Button>
              <Button variant="outline" onClick={stay}>
                See how sharing works
              </Button>
            </>
          }
        />
      </Section>

      <Section title="With a picture beside the words">
        <Hero
          as="h2"
          label={<Badge variant="outline">Beta</Badge>}
          heading="Give each team the apps it needs."
          description="Share an app with Finance, Operations or another selected group, and keep permission to use it separate from permission to change it."
          actions={<Button onClick={stay}>Bring an app</Button>}
          note="Connect the agent you already use; it sees only what your roles allow."
          image={<Placeholder />}
        />
      </Section>

      <Section title="Centred while narrow, beside its picture once wide">
        <Hero
          as="h2"
          align="auto"
          heading="You built it. We ship it."
          description="On a phone the words, the buttons and then the picture are centred, one under the other; once the hero is wide, the words start at the left with the picture beside them."
          actions={<Button onClick={stay}>Add to Claude Code</Button>}
          image={<Placeholder />}
        />
      </Section>

      <Section title="Centred, for a page with no picture">
        <Hero
          as="h2"
          align="center"
          label="For teams already building"
          heading="Build with the agent you already use."
          description="Connect it once, and it sees the projects your roles allow."
          actions={
            <>
              <Button onClick={stay}>Read the connector docs</Button>
              <Button variant="outline" onClick={stay}>
                See the tools
              </Button>
            </>
          }
        />
      </Section>

      <Section title="The smaller size, for a page that is not the first">
        <Hero
          as="h2"
          variant="medium"
          heading="What this does, and what it does not."
          description="Sign-in controls who can open an app. It does not control what they can do inside it."
        />
      </Section>
    </>
  );
}
