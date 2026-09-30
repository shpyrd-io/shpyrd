"use client";

import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Breadcrumbs>
          <BreadcrumbsItem href="#home" onClick={stay}>
            Home
          </BreadcrumbsItem>
          <BreadcrumbsItem href="#about" onClick={stay}>
            About
          </BreadcrumbsItem>
          <BreadcrumbsItem href="#team" onClick={stay} selected>
            Team
          </BreadcrumbsItem>
        </Breadcrumbs>
      </Section>
      <Section title="A level that is only a name">
        <Breadcrumbs>
          <BreadcrumbsItem href="#overview" onClick={stay}>
            Overview
          </BreadcrumbsItem>
          <BreadcrumbsItem>Foundations</BreadcrumbsItem>
          <BreadcrumbsItem href="#button" onClick={stay} selected>
            Button
          </BreadcrumbsItem>
        </Breadcrumbs>
      </Section>
      <Section title="Many levels go to the next line">
        <div className="max-w-xs">
          <Breadcrumbs>
            <BreadcrumbsItem href="#workspaces" onClick={stay}>
              Workspaces
            </BreadcrumbsItem>
            <BreadcrumbsItem href="#shpyrd" onClick={stay}>
              Shpyrd
            </BreadcrumbsItem>
            <BreadcrumbsItem href="#projects" onClick={stay}>
              Projects
            </BreadcrumbsItem>
            <BreadcrumbsItem href="#hello" onClick={stay}>
              Hello World
            </BreadcrumbsItem>
            <BreadcrumbsItem href="#releases" onClick={stay}>
              Releases
            </BreadcrumbsItem>
            <BreadcrumbsItem href="#v12" onClick={stay} selected>
              v12
            </BreadcrumbsItem>
          </Breadcrumbs>
        </div>
      </Section>
    </>
  );
}
