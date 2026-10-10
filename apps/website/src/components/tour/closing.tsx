// Slide 11: let everyone build, we run the rest. The way to start, and a
// 30-day pilot week by week.
import { BarChart3, CalendarCheck, Check, FolderInput, KeyRound, MessageCircle, Users } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { closing } from "@shpyrd/content/site/tour";
import { enterpriseContact } from "@shpyrd/content/site/offer";
import { DeployButton } from "@/components/deploy-button";
import { Lead, panel } from "./parts";

const weekIcons = [<KeyRound key="k" />, <FolderInput key="f" />, <Users key="u" />, <BarChart3 key="b" />];

export function Closing() {
  return (
    <div className="grid items-center gap-10 lg:grid-cols-[1.1fr_1fr]">
      <div className="grid gap-6">
        <h1 className="font-heading text-6xl leading-[1.04] font-semibold tracking-tight">
          {closing.heading[0]}
          <br />
          <span className="text-primary">{closing.heading[1]}</span>
        </h1>
        <Lead>{closing.lead}</Lead>
        <ul className="flex flex-wrap gap-x-5 gap-y-2 text-sm">
          {closing.points.map((p) => (
            <li key={p} className="flex items-center gap-1.5 [&_svg]:size-4 [&_svg]:text-primary">
              <Check />
              {p}
            </li>
          ))}
        </ul>
        <div className="flex flex-wrap gap-3">
          <DeployButton size="lg" />
          <Button asChild variant="outline" size="lg" icon={<MessageCircle />}>
            <a href={enterpriseContact.href}>{closing.contact}</a>
          </Button>
        </div>

        {/* The pilot, a line a week. */}
        <div className={cn(panel, "grid gap-3 p-5")}>
          <p className="flex items-center gap-2 text-sm font-medium [&_svg]:size-4 [&_svg]:text-primary">
            <CalendarCheck />
            {closing.pilot.title}
          </p>
          <Timeline>
            {closing.pilot.weeks.map((w, i) => (
              <TimelineItem
                key={w.title}
                condensed
                type="primary"
                icon={weekIcons[i]}
                className="animate-in fade-in slide-in-from-bottom-2 fill-mode-both duration-slow ease-enter"
                style={{ animationDelay: `${200 + i * 150}ms` }}
              >
                <p className="text-sm">
                  <span className="mr-2 font-mono text-[0.6875rem] tracking-[0.14em] text-primary uppercase">Week {i + 1}</span>
                  <span className="font-medium">{w.title}</span>
                  <span className="text-muted-foreground"> · {w.body}</span>
                </p>
              </TimelineItem>
            ))}
          </Timeline>
        </div>
      </div>

      {/* The yard at work, large: the rest, being run. */}
      <Shipyard className="mx-auto w-full max-w-[34rem] max-lg:hidden" />
    </div>
  );
}
