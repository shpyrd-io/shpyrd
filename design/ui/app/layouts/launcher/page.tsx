"use client";

import {
  ChevronDown,
  LayoutGrid,
  LogOut,
  Moon,
  Plus,
  Settings,
  Sun,
  UserRound,
} from "lucide-react";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@shpyrd/ui/components/dropdown-menu";
import { AppIcon } from "@shpyrd/ui/components/app-icons";
import { LauncherCard } from "@shpyrd/ui/components/launcher-card";
import { useTheme } from "@shpyrd/ui/lib/theme";
import { Section } from "../../section";
import { people } from "../../people";

// Where everyone lands after signing in: the applications of the
// workspace, centred on the page, and the settings floating in the
// corner. Made of the launcher card; what it shows is made up.

type App = React.ComponentProps<typeof LauncherCard>;

const settings = () => {};

const apps: App[] = [
  { name: "Corporate", description: "Operations of the day, finance and the team.", icon: <AppIcon name="briefcase" />, colour: "orange", url: "corporate.acme.com", tags: ["Finance", "Operations"] },
  { name: "People", description: "The calendar, the posts and the billing of events.", icon: <AppIcon name="calendar-days" />, colour: "green", url: "people.acme.shpyrd.app", tags: ["Events"] },
  { name: "Connect", description: "People, mailing and the reminders of the agency.", icon: <AppIcon name="users" />, colour: "blue", url: "connect.acme.shpyrd.app", access: "locked", tags: ["Sales", "Support"] },
  { name: "Dash TV", description: "The two business units on one screen, for the TV.", icon: <AppIcon name="layout-dashboard" />, colour: "violet", url: "dash.acme.internal", phase: "sleeping", exposure: "internal", access: "locked", tags: ["Board"] },
  { name: "Reports", description: "Builds the reports of the month, every night.", tone: "blue", phase: "failed", exposure: "internal", tags: ["Finance"] },
  { name: "Docs", description: "What is written about the agency, for everyone.", icon: <AppIcon name="book-open" />, colour: "teal", url: "docs.acme.com", phase: "deploying" },
  { name: "Mail", description: "The mail of the agency, on its own.", icon: <AppIcon name="mail" />, colour: "grey", url: "mail.acme.internal", exposure: "internal", access: "locked" },
  { name: "Shop", description: "The store of the agency's own products.", icon: <AppIcon name="store" />, colour: "pink", url: "shop.acme.com", tags: ["Sales"] },
  { name: "Tickets", description: "What the clients ask, and who answers.", icon: <AppIcon name="headset" />, colour: "indigo", url: "tickets.acme.shpyrd.app", access: "locked", tags: ["Support"] },
  { name: "Chat", description: "The rooms of the teams.", icon: <AppIcon name="message-square" />, colour: "cyan", url: "chat.acme.internal", exposure: "internal", access: "locked", tags: ["Everyone"] },
  { name: "Garden", description: "The orders of the garden centre, and its deliveries.", picture: "/samples/picture.png", url: "garden.acme.com", tags: ["Orders"] },
  { name: "Metrics", description: "Collects the numbers of every application, each hour.", tone: "orange", exposure: "internal", tags: ["Platform"] },
];

export default function Page() {
  return (
    <>
      {[3, 5, 12].map((count) => (
        <Section key={count} title={`${count} applications`}>
          <Launcher apps={apps.slice(0, count)} />
        </Section>
      ))}
      <Section title="None yet, and the person may create one">
        <Launcher apps={[]} />
      </Section>
      <Section title="None yet, and the person may not">
        <Launcher apps={[]} canCreate={false} />
      </Section>
    </>
  );
}

// The page itself: nothing around it but the brand, the small controls
// of the person and the settings.
function Launcher({ apps, canCreate = true }: { apps: App[]; canCreate?: boolean }) {
  const [theme, setTheme] = useTheme();
  const [me] = people;
  return (
    <div className="relative overflow-hidden rounded-xl bg-background ring-1 ring-foreground/10">
      <div className="absolute top-5 right-5 flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon"
          icon={theme === "dark" ? <Moon /> : <Sun />}
          aria-label={theme === "dark" ? "Light theme" : "Dark theme"}
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" icon={<Avatar size={20} src={me.src} alt="" />} iconEnd={<ChevronDown />}>
              {me.alt}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel className="font-normal">
              <div className="text-sm font-medium">{me.alt}</div>
              <div className="text-xs text-muted-foreground">signed in with GitHub</div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem>
              <UserRound /> Your account
            </DropdownMenuItem>
            <DropdownMenuItem>
              <LogOut /> Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {canCreate && (
          <Button size="icon-lg" icon={<Plus />} aria-label="New project" onClick={settings} className="ml-2" />
        )}
        <Button
          variant="outline"
          size="icon-lg"
          icon={<Settings />}
          aria-label="Settings of the workspace"
          onClick={settings}
          className={canCreate ? undefined : "ml-2"}
        />
      </div>
      <div className="mx-auto grid max-w-6xl justify-items-center gap-10 px-6 pt-20 pb-16 @3xl/page-layout:px-8 @3xl/page-layout:pt-20 @3xl/page-layout:pb-16">
        <div className="grid justify-items-center gap-3 text-center">
          <LogoMark className="size-14" />
          <h2 className="font-heading text-2xl font-medium">Acme</h2>
        </div>
        {apps.length > 0 ? (
          <div className="flex flex-wrap justify-center gap-4">
            {apps.map((app) => (
              <LauncherCard key={app.name} {...app} onSettings={settings} />
            ))}
          </div>
        ) : canCreate ? (
          <Blankslate
            graphic={<LayoutGrid />}
            title="No application yet"
            description="A project is an application and everything it needs to run. Make the first one, and it appears here for everyone who may open it."
            action={
              <Button icon={<Plus />} onClick={settings}>
                New project
              </Button>
            }
          />
        ) : (
          <Blankslate
            graphic={<LayoutGrid />}
            title="Your applications will appear here"
            description="When a project of this workspace lets you in, it shows up on this page."
          />
        )}
      </div>
      {/* The version, in the corner, as light as it can be and still be read. */}
      <span className="absolute right-5 bottom-4 font-mono text-[11px] text-muted-foreground/60">v0.42.0</span>
    </div>
  );
}
