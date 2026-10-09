"use client";

import { LogoSuite } from "@shpyrd/ui/components/logo-suite";
import { Section } from "../../section";
import { fictionalLogos } from "../logos";

export default function Page() {
  return (
    <>
      <Section title="Centred, with a bar of logos under it">
        <LogoSuite
          heading="Teams that ship with shpyrd"
          description="From two people to two thousand, they put their tools online without a platform team."
          logos={fictionalLogos.slice(0, 6)}
        />
      </Section>

      <Section title="Starting at the edge, the bar spread across">
        <LogoSuite
          align="start"
          heading="Works with what you already use"
          description="Sign in with the identity you have and deploy from the code you keep."
          logos={fictionalLogos.slice(0, 6)}
        />
      </Section>

      <Section title="A heading alone">
        <LogoSuite heading="Trusted by builders everywhere" logos={fictionalLogos.slice(0, 5)} />
      </Section>

      <Section title="Too many to stand in a bar, so they go by">
        <LogoSuite
          marquee
          heading="Used in every kind of company"
          description="Finance, operations, support and the teams in between."
          logos={fictionalLogos}
        />
      </Section>
    </>
  );
}
