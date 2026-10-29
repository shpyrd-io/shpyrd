"use client";

// The tour's slides, in order (src/content/tour.ts names them).
import { Access } from "./access-scenarios";
// Slide 10 turning from today to with shpyrd (Giovani, 2026-10-29); the
// earlier one stays in audiences.tsx.
import { AudiencesStory } from "./audiences-story";
// Slide 11 read as the deck's conversion (Giovani, 2026-10-29); the earlier
// ones stay in closing-story.tsx and closing.tsx.
import { ClosingConvert } from "./closing-convert";
import { Deck } from "./deck";
// Slide 8 told in steps (Giovani, 2026-10-28); the earlier ones stay in
// where-it-runs.tsx and delivery.tsx.
import { WhereItRunsStory } from "./where-it-runs-story";
import { Flow } from "./flow";
// Slide 9 with the trail moving the list (Giovani, 2026-10-29); the earlier
// one stays in governance.tsx.
import { GovernanceStory } from "./governance-story";
import { Opening } from "./opening";
import { Platform } from "./platform";
import { Problem } from "./problem";
// Slide 7 in blocks, as the home's cost film (Giovani, 2026-10-28); the
// earlier slide stays in sleep.tsx, the line proposal in sleep-day.tsx.
import { SleepDayBlocks } from "./sleep-day-blocks";
import { Xray } from "./xray";

export function Tour() {
  return (
    <Deck
      slides={[
        (go) => <Opening next={() => go(1)} />,
        () => <Problem />,
        () => <Xray />,
        () => <Platform />,
        () => <Flow />,
        () => <Access />,
        () => <SleepDayBlocks />,
        () => <WhereItRunsStory />,
        () => <GovernanceStory />,
        (go) => <AudiencesStory go={go} />,
        () => <ClosingConvert />,
      ]}
    />
  );
}
