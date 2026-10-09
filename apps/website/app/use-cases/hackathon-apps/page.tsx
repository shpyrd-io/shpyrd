import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { HackathonNext } from "@/components/use-case-hackathon-apps";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as AfterTheDemo } from "./_parts/after-the-demo";
import { Part as RolloutChecklist } from "./_parts/rollout-checklist";
import { Part as ForOrganisers } from "./_parts/for-organisers";

// Apps from a hackathon, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "Apps from a hackathon",
  description:
    "Your hackathon made dozens of apps. Keep the few people want, and put them in front of their colleagues next week.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="Apps from a hackathon"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "after-the-demo", title: "After the demo", content: <AfterTheDemo /> },
          { slug: "rollout-checklist", title: "Rollout checklist", content: <RolloutChecklist /> },
          { slug: "for-organisers", title: "For organisers", content: <ForOrganisers /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <HackathonNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
