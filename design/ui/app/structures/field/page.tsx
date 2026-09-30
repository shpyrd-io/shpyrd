import { Search } from "lucide-react";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="A label and its field">
        <Field label="Project name" className="max-w-sm">
          <Input placeholder="hello-world" />
        </Field>
      </Section>
      <Section title="With a hint">
        <Field
          label="Address"
          hint="Lowercase letters, digits and dashes. It cannot be changed later."
          className="max-w-sm"
        >
          <Input suffix=".platform.shpyrd.app" placeholder="hello" />
        </Field>
      </Section>
      <Section title="Required">
        <Field label="Owner" required className="max-w-sm">
          <Input type="email" placeholder="you@example.com" />
        </Field>
      </Section>
      <Section title="With an error">
        <Field
          label="Address"
          hint="Lowercase letters, digits and dashes."
          error="An address cannot have spaces."
          className="max-w-sm"
        >
          <Input suffix=".platform.shpyrd.app" defaultValue="hello world" />
        </Field>
      </Section>
      <Section title="The label only for who cannot see">
        <Field label="Search the projects" labelHidden className="max-w-sm">
          <Input type="search" icon={<Search />} placeholder="Search" />
        </Field>
      </Section>
    </>
  );
}
