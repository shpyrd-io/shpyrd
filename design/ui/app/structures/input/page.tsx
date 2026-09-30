import { ArrowRight, Earth, Lock, Mail, Search, UserRound } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Stack } from "@shpyrd/ui/components/stack";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Field label="Project name" className="max-w-sm">
          <Input placeholder="hello-world" />
        </Field>
      </Section>
      <Section title="With an icon">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Field label="Before the text">
            <Input type="search" icon={<Search />} placeholder="Search" />
          </Field>
          <Field label="After the text">
            <Input type="email" iconEnd={<Mail />} placeholder="you@example.com" />
          </Field>
        </div>
      </Section>
      <Section title="With a prefix and a suffix">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Field label="A prefix">
            <Input prefix="https://" placeholder="example.com" />
          </Field>
          <Field label="A suffix">
            <Input suffix=".platform.shpyrd.app" placeholder="hello" />
          </Field>
          <Field label="Both">
            <Input inputMode="decimal" prefix="R$" suffix="a month" placeholder="0,00" />
          </Field>
          <Field label="An icon and a suffix">
            <Input icon={<Earth />} suffix=".shpyrd.app" placeholder="hello" />
          </Field>
        </div>
      </Section>
      <Section title="Sizes">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-3">
          <Field label="Small">
            <Input size="sm" icon={<Search />} placeholder="Search" />
          </Field>
          <Field label="Default">
            <Input icon={<Search />} placeholder="Search" />
          </Field>
          <Field label="Large">
            <Input size="lg" icon={<Search />} placeholder="Search" />
          </Field>
        </div>
      </Section>
      <Section title="A form alone on a page: large, the icon set apart by a line">
        <Card className="max-w-sm">
          <CardContent>
            <Stack gap="normal">
              <Field label="Email address">
                <Input size="lg" type="email" icon={<UserRound />} divider placeholder="you@example.com" />
              </Field>
              <Field label="Password">
                <Input size="lg" type="password" icon={<Lock />} divider placeholder="Password" />
              </Field>
              <Button size="xl" className="w-full" iconEnd={<ArrowRight />}>
                Sign in
              </Button>
            </Stack>
          </CardContent>
        </Card>
      </Section>
      <Section title="States">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Field label="Not valid">
            <Input aria-invalid suffix=".shpyrd.app" defaultValue="hello world" />
          </Field>
          <Field label="Disabled">
            <Input disabled icon={<Search />} placeholder="Search" />
          </Field>
        </div>
      </Section>
    </>
  );
}
