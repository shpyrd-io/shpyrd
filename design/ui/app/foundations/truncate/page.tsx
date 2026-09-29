import { Truncate } from "@shpyrd/ui/components/truncate";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default: 125 pixels">
        <Truncate>hello-world.platform.shpyrd.app</Truncate>
      </Section>
      <Section title="As wide as its place">
        <div className="max-w-60 rounded-lg p-3 ring-1 ring-foreground/10">
          <Truncate maxWidth="100%">
            A project is an application and everything it needs to run.
          </Truncate>
        </div>
      </Section>
      <Section title="A width of its own">
        <Truncate maxWidth="10ch">hello-world.platform.shpyrd.app</Truncate>
        <Truncate maxWidth={180}>hello-world.platform.shpyrd.app</Truncate>
      </Section>
      <Section title="Along the line of a text">
        <p className="text-sm text-muted-foreground">
          The release{" "}
          <Truncate as="span" inline maxWidth="12ch" className="font-mono text-foreground">
            7f3c9a1e5b2d4c8f9a0b
          </Truncate>{" "}
          went out two hours ago.
        </p>
      </Section>
      <Section title="The whole text while the pointer is over it">
        <Truncate expandable>hello-world.platform.shpyrd.app</Truncate>
      </Section>
    </>
  );
}
