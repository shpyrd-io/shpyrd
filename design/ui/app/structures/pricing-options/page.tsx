"use client";

import { Button } from "@shpyrd/ui/components/button";
import { PricingOption, PricingOptions } from "@shpyrd/ui/components/pricing-options";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const free = {
  heading: "Free",
  description: "One app, online and shared, to see it working.",
  price: "0",
  trailingText: "forever",
  features: [
    { children: "1 project, 1 instance, up to 256 MiB of memory" },
    { children: "Its own address, with your sign-in in front" },
    { children: "Sleeps after 10 minutes without visits" },
  ],
};

const starter = {
  heading: "Starter",
  description: "Your team's apps, as many as you need, billed by what they use.",
  price: "5",
  trailingText: "a month minimum, then usage",
  features: [
    { children: "As many projects and instances as you need" },
    { children: "Every size, up to dedicated compute" },
    { children: "Databases stay awake" },
  ],
};

export default function Page() {
  return (
    <>
      <Section title="Default, divided by a line">
        <PricingOptions>
          <PricingOption {...free} actions={<Button onClick={stay}>Get started</Button>} />
          <PricingOption
            {...starter}
            actions={<Button variant="outline" onClick={stay}>Talk to us</Button>}
            message="Opened by hand while in beta."
          />
        </PricingOptions>
      </Section>

      <Section title="Cards">
        <PricingOptions variant="cards">
          <PricingOption {...free} actions={<Button onClick={stay}>Get started</Button>} />
          <PricingOption
            {...starter}
            label="For teams"
            actions={<Button variant="outline" onClick={stay}>Talk to us</Button>}
            footnote="Prices in USD. Usage is billed by the hour."
          />
        </PricingOptions>
      </Section>

      <Section title="Centred">
        <PricingOptions variant="cards" align="center">
          <PricingOption {...free} actions={<Button onClick={stay}>Get started</Button>} />
          <PricingOption {...starter} actions={<Button variant="outline" onClick={stay}>Talk to us</Button>} />
        </PricingOptions>
      </Section>

      <Section title="Three, with what a plan leaves out and a discount">
        <PricingOptions variant="cards">
          <PricingOption
            {...free}
            features={[...free.features, { children: "Databases that stay awake", variant: "excluded" }]}
            actions={<Button variant="outline" onClick={stay}>Get started</Button>}
          />
          <PricingOption
            {...starter}
            label="Most chosen"
            actions={<Button onClick={stay}>Talk to us</Button>}
          />
          <PricingOption
            heading="Team"
            description="For a company that runs many apps."
            price="16"
            originalPrice="20"
            trailingText="per seat, per month"
            features={[{ children: "Everything in Starter" }, { children: "Your company's sign-in" }]}
            actions={<Button variant="outline" onClick={stay}>Talk to us</Button>}
          />
        </PricingOptions>
      </Section>

      <Section title="With usage pricing">
        <PricingOptions variant="cards">
          <PricingOption
            {...free}
            usage={[
              { name: "Compute", note: "by actual use", value: "Included" },
              { name: "Memory", note: "reserved, while awake", value: "Included" },
              { name: "Storage", note: "awake or asleep", value: "Included" },
              { name: "Traffic out", note: "sent to visitors", value: "Included" },
            ]}
            actions={<Button onClick={stay}>Get started</Button>}
          />
          <PricingOption
            {...starter}
            usage={[
              { name: "Compute", note: "by actual use", value: "$0.02 / compute unit-hour" },
              { name: "Memory", note: "reserved, while awake", value: "$0.005 / GiB-hour" },
              { name: "Storage", note: "awake or asleep", value: "$0.10 / GiB-month" },
              { name: "Traffic out", note: "sent to visitors", value: "$0.05 / GiB" },
            ]}
            actions={<Button variant="outline" onClick={stay}>Talk to us</Button>}
            footnote="Prices in USD."
          />
        </PricingOptions>
      </Section>

      <Section title="One plan">
        <PricingOptions variant="cards" className="max-w-md">
          <PricingOption {...free} actions={<Button onClick={stay}>Get started</Button>} />
        </PricingOptions>
      </Section>
    </>
  );
}
