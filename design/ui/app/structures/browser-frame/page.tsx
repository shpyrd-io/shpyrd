import { Button } from "@shpyrd/ui/components/button";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Card } from "@shpyrd/ui/components/card";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="A page at its address, with the padlock">
        <BrowserFrame address="purchases.acme.shpyrd.app" className="max-w-md">
          <div className="grid place-items-center bg-muted/40 px-6 py-12">
            <Card className="w-full max-w-80 gap-4 p-6 text-center">
              <p className="font-heading font-medium">Sign in to open Purchase requests</p>
              <p className="text-sm text-muted-foreground">Finance and Operations can open this app.</p>
              <Button className="w-full">Use your company account</Button>
            </Card>
          </div>
        </BrowserFrame>
      </Section>

      <Section title="Without the padlock, for an address that is not secure">
        <BrowserFrame address="localhost:3000" secure={false} className="max-w-md">
          <p className="px-5 py-4 text-sm text-muted-foreground">Works for you, on your laptop.</p>
        </BrowserFrame>
      </Section>

      <Section title="An address too long for its room is cut">
        <BrowserFrame
          address="onboarding-checklist.people.acme.shpyrd.app/new-starters/2026/october"
          className="max-w-xs"
        >
          <p className="px-5 py-4 text-sm text-muted-foreground">The frame keeps its width.</p>
        </BrowserFrame>
      </Section>
    </>
  );
}
