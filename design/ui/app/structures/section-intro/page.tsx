"use client";

import { Button } from "@shpyrd/ui/components/button";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

export default function Page() {
  return (
    <>
      <Section title="Only the heading">
        <SectionIntro as="h3" heading="Three things you get" />
      </Section>

      <Section title="With a description">
        <SectionIntro
          as="h3"
          heading="Where it runs, and who runs it"
          description="On a Kubernetes cluster your company controls: locally while you try it, and on your own cloud when it matters."
        />
      </Section>

      <Section title="With a label over it, and somewhere to read more">
        <SectionIntro
          as="h3"
          label="For the person who maintains it"
          heading="Manage it over time"
          description="Every deploy, config change and rollback is a numbered release."
          link={
            <Button variant="link" className="px-0" onClick={stay}>
              Read the deployment docs
            </Button>
          }
        />
      </Section>

      <Section title="Centred, where the section has no picture beside it">
        <SectionIntro
          as="h3"
          align="center"
          label="What this does not do"
          heading="Sign-in controls who can open an app"
          description="It does not control what they can do inside it."
        />
      </Section>

      <Section title="The smaller size, for a section inside a section">
        <SectionIntro as="h3" variant="medium" heading="What people put in it" />
      </Section>
    </>
  );
}
