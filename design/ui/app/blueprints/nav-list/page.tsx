"use client";

import { useState } from "react";
import {
  BookOpen,
  Boxes,
  ExternalLink,
  History,
  KeyRound,
  LayoutDashboard,
  Monitor,
  Palette,
  Pin,
  ScrollText,
  User,
} from "lucide-react";
import { KeybindingHint } from "@shpyrd/ui/components/keybinding-hint";
import {
  NavList,
  NavListAction,
  NavListDivider,
  NavListGroup,
  NavListItem,
  NavListSubNav,
} from "@shpyrd/ui/components/nav-list";
import { Section } from "../../section";

export default function Page() {
  const settings = useCurrent("profile");
  const project = useCurrent("web");
  const projects = useCurrent("docs001");
  const [pinned, setPinned] = useState("none");
  return (
    <>
      <Section title="With a heading and groups">
        <Frame>
          <NavList heading="Settings">
            <NavListGroup title="Account">
              <NavListItem {...settings("profile")} icon={<User />}>
                Profile
              </NavListItem>
              <NavListItem {...settings("appearance")} icon={<Palette />}>
                Appearance
              </NavListItem>
            </NavListGroup>
            <NavListGroup title="Security">
              <NavListItem {...settings("password")} icon={<KeyRound />}>
                Password and authentication
              </NavListItem>
              <NavListItem {...settings("sessions")} icon={<Monitor />}>
                Sessions
              </NavListItem>
            </NavListGroup>
          </NavList>
        </Frame>
      </Section>
      <Section title="Items that open, and what comes after the text">
        <Frame>
          <NavList aria-label="Project">
            <NavListItem {...project("overview")} icon={<LayoutDashboard />}>
              Overview
            </NavListItem>
            <NavListSubNav title="Processes" icon={<Boxes />} defaultOpen>
              <NavListItem {...project("web")} iconEnd="2">
                web
              </NavListItem>
              <NavListItem {...project("worker")} iconEnd="1">
                worker
              </NavListItem>
            </NavListSubNav>
            <NavListSubNav title="Releases" icon={<History />} iconEnd="12">
              <NavListItem {...project("v12")}>v12</NavListItem>
              <NavListItem {...project("v11")}>v11</NavListItem>
            </NavListSubNav>
            <NavListItem
              {...project("logs")}
              icon={<ScrollText />}
              iconEnd={<KeybindingHint keys="g l" size="sm" />}
            >
              Logs
            </NavListItem>
          </NavList>
        </Frame>
      </Section>
      <Section title="With an action beside each item">
        <Frame>
          <NavList aria-label="Projects">
            {[
              ["docs001", "Docs 001"],
              ["hello", "hello"],
              ["helloworld", "Hello World"],
            ].map(([name, title]) => (
              <NavListItem
                key={name}
                {...projects(name)}
                action={
                  <NavListAction
                    label={`Pin ${title}`}
                    icon={<Pin />}
                    onClick={() => setPinned(title)}
                  />
                }
              >
                {title}
              </NavListItem>
            ))}
            <NavListDivider />
            <NavListItem {...projects("docs")} icon={<BookOpen />} iconEnd={<ExternalLink />}>
              Documentation
            </NavListItem>
          </NavList>
        </Frame>
        <p className="text-sm text-muted-foreground">The last one pinned: {pinned}</p>
      </Section>
    </>
  );
}

function Frame({ children }: { children: React.ReactNode }) {
  return <div className="max-w-72 rounded-xl py-3 ring-1 ring-foreground/10">{children}</div>;
}

// The props of an item of a list whose current page follows the clicks.
function useCurrent(first: string) {
  const [current, setCurrent] = useState(first);
  return (name: string) => ({
    href: `#${name}`,
    "aria-current": current === name ? ("page" as const) : undefined,
    onClick: (event: React.MouseEvent) => {
      event.preventDefault();
      setCurrent(name);
    },
  });
}
