"use client";

import { ArrowLeft, Plus, RotateCw, UsersRound } from "lucide-react";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Button } from "@shpyrd/ui/components/button";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

// The headings here are inside a page that has its own, so they are `h2`:
// a page has one `h1`.
export default function Page() {
  return (
    <>
      <Section title="Only the title">
        <PageHeading as="h2" title="Projects" />
      </Section>
      <Section title="With an icon, a description and an action">
        <PageHeading
          as="h2"
          icon={<UsersRound />}
          title="Teams"
          description="Groups of users that projects grant roles to. A team can also hold a platform role."
          actions={<Button icon={<Plus />}>New team</Button>}
        />
      </Section>
      <Section title="With what comes after the title, and where the page is">
        <PageHeading
          as="h2"
          context={
            <Breadcrumbs>
              <BreadcrumbsItem href="#projects" onClick={stay}>
                Projects
              </BreadcrumbsItem>
              <BreadcrumbsItem href="#hello" onClick={stay} selected>
                Hello World
              </BreadcrumbsItem>
            </Breadcrumbs>
          }
          title="Hello World"
          iconEnd={
            <>
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                hello-world
              </code>
              <StatusBadge type="success">Running</StatusBadge>
            </>
          }
          description="Release v12, two instances ready."
          actions={
            <>
              <Button variant="outline" icon={<RotateCw />}>
                Restart
              </Button>
              <Button>Deploy</Button>
            </>
          }
          border
        />
      </Section>
      <Section title="A link to the page over it">
        <PageHeading
          as="h2"
          context={
            <Button variant="link" size="sm" className="h-auto p-0" icon={<ArrowLeft />} asChild>
              <a href="#projects" onClick={stay}>
                Projects
              </a>
            </Button>
          }
          title="Releases"
        />
      </Section>
      <Section title="Sizes">
        <PageHeading as="h2" variant="large" title="Large, for what a person named" />
        <PageHeading as="h2" title="Medium, for most pages" />
        <PageHeading as="h2" variant="subtitle" title="Subtitle, under another heading" />
      </Section>
    </>
  );
}
