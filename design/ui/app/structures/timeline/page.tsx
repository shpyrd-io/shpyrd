import {
  CircleCheck,
  CircleX,
  GitCommitHorizontal,
  Hammer,
  Rocket,
  RotateCw,
  Undo2,
  UserPlus,
} from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Timeline, TimelineBreak, TimelineItem } from "@shpyrd/ui/components/timeline";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Timeline>
          <TimelineItem icon={<GitCommitHorizontal />}>
            <strong>Patrick</strong> pushed <a href="#commit">7f3c9a1</a> to main
          </TimelineItem>
          <TimelineItem icon={<Hammer />}>The release v12 was built in 48 seconds</TimelineItem>
          <TimelineItem icon={<Rocket />}>
            The release <strong>v12</strong> went out, two instances ready
          </TimelineItem>
        </Timeline>
      </Section>
      <Section title="Types">
        <Timeline>
          <TimelineItem icon={<UserPlus />}>Neutral: someone joined the workspace</TimelineItem>
          <TimelineItem type="primary" icon={<Rocket />}>
            Primary: a release went out
          </TimelineItem>
          <TimelineItem type="success" icon={<CircleCheck />}>
            Success: the instances are ready
          </TimelineItem>
          <TimelineItem type="info" icon={<Hammer />}>
            Info: a build started
          </TimelineItem>
          <TimelineItem type="warning" icon={<RotateCw />}>
            Warning: an instance restarted
          </TimelineItem>
          <TimelineItem type="error" icon={<CircleX />}>
            Error: the release failed
          </TimelineItem>
        </Timeline>
      </Section>
      <Section title="The line clipped at both ends">
        <Timeline clip>
          <TimelineItem icon={<GitCommitHorizontal />}>The first thing that happened</TimelineItem>
          <TimelineItem icon={<Hammer />}>What happened after it</TimelineItem>
          <TimelineItem icon={<Rocket />}>The last thing that happened</TimelineItem>
        </Timeline>
      </Section>
      <Section title="Condensed">
        <Timeline>
          <TimelineItem condensed icon={<GitCommitHorizontal />}>
            <a href="#a">7f3c9a1</a> the button takes an icon
          </TimelineItem>
          <TimelineItem condensed icon={<GitCommitHorizontal />}>
            <a href="#b">2ebf761</a> a dropdown button
          </TimelineItem>
          <TimelineItem condensed icon={<GitCommitHorizontal />}>
            <a href="#c">bf7f630</a> the card takes a shadow
          </TimelineItem>
        </Timeline>
      </Section>
      <Section title="With a break">
        <Timeline>
          <TimelineItem type="error" icon={<CircleX />}>
            The release <strong>v11</strong> failed: the instances did not answer
          </TimelineItem>
          <TimelineBreak />
          <TimelineItem icon={<Hammer />}>The release v12 was built</TimelineItem>
          <TimelineItem type="success" icon={<Rocket />}>
            The release <strong>v12</strong> went out
          </TimelineItem>
        </Timeline>
      </Section>
      <Section title="With an action">
        <Timeline clip>
          <TimelineItem
            type="success"
            icon={<Rocket />}
            actions={
              <Button variant="outline" size="sm" icon={<Undo2 />}>
                Go back to it
              </Button>
            }
          >
            The release <strong>v11</strong> went out
            <div className="text-xs">Yesterday, by Patrick</div>
          </TimelineItem>
          <TimelineItem type="primary" icon={<Rocket />}>
            The release <strong>v12</strong> went out
            <div className="text-xs">Two hours ago, by Patrick</div>
          </TimelineItem>
        </Timeline>
      </Section>
    </>
  );
}
