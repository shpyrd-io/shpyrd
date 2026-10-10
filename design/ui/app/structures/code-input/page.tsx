"use client";

import * as React from "react";
import { CodeInput } from "@shpyrd/ui/components/code-input";
import { Section } from "../../section";

function Example(props: Partial<React.ComponentProps<typeof CodeInput>>) {
  const [value, setValue] = React.useState(props.value ?? "");
  return <CodeInput {...props} value={value} onChange={setValue} />;
}

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Example />
      </Section>
      <Section title="Four digits">
        <Example length={4} />
      </Section>
      <Section title="A code that was wrong">
        <Example value="123456" invalid />
      </Section>
      <Section title="While it is checked">
        <Example value="123456" disabled />
      </Section>
    </>
  );
}
