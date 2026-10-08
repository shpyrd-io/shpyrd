"use client";

import { BigNumbers } from "@shpyrd/ui/components/big-numbers";
import { Section } from "../../section";

const all = [
  { value: "20M+", caption: "requests served to the people who sign in" },
  { value: "3M+", caption: "releases deployed, each one numbered" },
  { value: "12k", caption: "apps put online by teams that build with AI" },
  { value: "99.9%", caption: "of the time, the app is there when needed" },
];

export default function Page() {
  return (
    <>
      <Section title="Four figures, with an eyebrow and a statement">
        <BigNumbers
          eyebrow="Trusted at scale"
          lead="The apps your team builds deserve to stay up."
          statement="shpyrd runs them behind a sign-in, for the people who need them, all day."
          figures={all}
        />
      </Section>

      <Section title="Two figures">
        <BigNumbers eyebrow="Backed by giants" lead="Built to last." statement="Teams of every size rely on it." figures={all.slice(0, 2)} />
      </Section>

      <Section title="Three figures, no eyebrow">
        <BigNumbers lead="Numbers, plainly." figures={all.slice(0, 3)} />
      </Section>
    </>
  );
}
