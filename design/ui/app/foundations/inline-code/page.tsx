import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="In a text">
        <p className="max-w-prose text-sm">
          Deploy what is in the folder with <InlineCode>shpyrd deploy</InlineCode>. The project
          reads its port from <InlineCode>PORT</InlineCode> and answers at{" "}
          <InlineCode>hello-world.platform.shpyrd.app</InlineCode>.
        </p>
      </Section>
      <Section title="In a heading: as big as the text around it">
        <h3 className="font-heading text-2xl font-medium">
          Follow a project with <InlineCode wrap={false}>shpyrd logs</InlineCode>
        </h3>
      </Section>
      <Section title="What is long goes to the next line; what is short may stay together">
        <p className="max-w-xs text-sm">
          A long command wraps when it has to:{" "}
          <InlineCode>shpyrd deploy --project hello-world --process web --wait</InlineCode>. A
          short one stays together: <InlineCode wrap={false}>/healthz</InlineCode>.
        </p>
      </Section>
    </>
  );
}
