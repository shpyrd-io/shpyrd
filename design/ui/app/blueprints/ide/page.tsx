import { IDE } from "@shpyrd/ui/components/ide";
import { Section } from "../../section";
import { deploy, long, manifest, server } from "../../code-samples";

export default function Page() {
  return (
    <>
      <Section title="Several files">
        <IDE
          files={[
            { name: "server.js", code: server },
            { name: "shpyrd.yaml", code: manifest },
            { name: "deploy.sh", code: deploy },
          ]}
        />
      </Section>
      <Section title="One file">
        <IDE files={[{ name: "shpyrd.yaml", code: manifest }]} />
      </Section>
      <Section title="Without the tabs: only the code">
        <IDE code={manifest} language="yaml" />
      </Section>
      <Section title="Without the tabs and without the numbers of the lines">
        <IDE code={deploy} language="sh" showLineNumbers={false} />
      </Section>
      <Section title="A file, without its name">
        <IDE files={[{ name: "server.js", code: server }]} tabs={false} />
      </Section>
      <Section title="With the tabs, without the numbers of the lines">
        <IDE files={[{ name: "deploy.sh", code: deploy }]} showLineNumbers={false} />
      </Section>
      <Section title="No taller than 200 pixels: the code scrolls">
        <IDE files={[{ name: "release.js", code: long }]} height={200} />
      </Section>
    </>
  );
}
