"use client";

import { Plus, Rocket, RotateCw, Search } from "lucide-react";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { Button } from "@shpyrd/ui/components/button";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Every part">
        <Blankslate
          border
          graphic={<Rocket />}
          title="No projects yet"
          description="A project is an application and everything it needs to run. Make the first one and it shows up here."
          action={<Button icon={<Plus />}>New project</Button>}
          secondaryAction={
            <Button variant="link" asChild>
              <a href="#projects" onClick={(e) => e.preventDefault()}>
                Read about projects
              </a>
            </Button>
          }
        />
      </Section>
      <Section title="Fewer parts">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Blankslate
            border
            graphic={<Search />}
            title="Nothing matches “docs”"
            description="Try another word, or fewer of them."
          />
          <Blankslate
            title="No logs in the last hour"
            action={
              <Button variant="outline" size="sm" icon={<RotateCw />}>
                Look again
              </Button>
            }
          />
        </div>
      </Section>
    </>
  );
}
