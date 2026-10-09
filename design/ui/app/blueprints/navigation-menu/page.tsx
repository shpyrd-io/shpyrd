"use client";

import { Bot, Briefcase, Handshake, ShieldCheck, SquareTerminal, Trophy, Wrench } from "lucide-react";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "@shpyrd/ui/components/navigation-menu";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const who = [
  { title: "For developers", icon: <SquareTerminal />, description: "Push code, get a URL, on shpyrd cloud." },
  { title: "For IT teams", icon: <ShieldCheck />, description: "One accepted place for the apps people build." },
  { title: "For FDE partners", icon: <Handshake />, description: "The same setup behind every client delivery." },
];
const what = [
  { title: "Internal tools", icon: <Wrench />, description: "Trackers, dashboards and approvals, in your team's day." },
  { title: "Apps from a hackathon", icon: <Trophy />, description: "Keep the few people want, from Monday on." },
  { title: "Agents and workers", icon: <Bot />, description: "What runs without a web page, off your laptop." },
  { title: "Client apps", icon: <Briefcase />, description: "Built by you, opened with their own sign-in." },
];

function Column({ label, items }: { label: string; items: typeof who }) {
  return (
    <div className="grid content-start gap-0.5">
      <p className="px-2.5 pt-1.5 pb-1 text-xs font-medium text-muted-foreground">{label}</p>
      {items.map((item) => (
        <NavigationMenuLink key={item.title} href="#" onClick={stay} title={item.title} description={item.description} icon={item.icon} />
      ))}
    </div>
  );
}

export default function Page() {
  return (
    <>
      <Section title="Words in a bar that open one shared panel; it eases to each menu's size">
        <div className="h-96 rounded-xl border p-3">
          <NavigationMenu>
            <NavigationMenuList>
              <NavigationMenuItem>
                <NavigationMenuTrigger>Solutions</NavigationMenuTrigger>
                <NavigationMenuContent>
                  <div className="grid grid-cols-[repeat(2,minmax(15rem,1fr))] gap-x-2">
                    <Column label="For" items={who} />
                    <Column label="What you're shipping" items={what} />
                  </div>
                </NavigationMenuContent>
              </NavigationMenuItem>
              <NavigationMenuItem>
                <NavigationMenuTrigger>Resources</NavigationMenuTrigger>
                <NavigationMenuContent>
                  <div className="grid w-72 gap-0.5">
                    <NavigationMenuLink href="#" onClick={stay} title="Docs" description="Every command and every setting." />
                    <NavigationMenuLink href="#" onClick={stay} title="Getting Started" description="Telling your agent who it's for." />
                  </div>
                </NavigationMenuContent>
              </NavigationMenuItem>
              <NavigationMenuItem>
                <NavigationMenuLink
                  href="#"
                  onClick={stay}
                  className="h-7 items-center rounded-md px-2.5 py-0 text-[0.8rem] font-medium text-muted-foreground hover:text-foreground"
                >
                  Pricing
                </NavigationMenuLink>
              </NavigationMenuItem>
            </NavigationMenuList>
          </NavigationMenu>
        </div>
      </Section>
    </>
  );
}
