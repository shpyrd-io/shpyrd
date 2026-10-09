import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { AgentsNext } from "@/components/use-case-agents-and-workers";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as WhyNotYourLaptop } from "./_parts/why-not-your-laptop";
import { Part as MoveItOffYourLaptop } from "./_parts/move-it-off-your-laptop";
import { Part as WebPageAndWorker } from "./_parts/web-page-and-worker";

// Agents and workers, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "Agents and workers",
  description:
    "The agent that reads your inbox, the worker behind a queue: things without a web page, kept running, with their secrets, logs and releases.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="Agents and workers"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "why-not-your-laptop", title: "Why not your laptop", content: <WhyNotYourLaptop /> },
          { slug: "move-it-off-your-laptop", title: "Move it off your laptop", content: <MoveItOffYourLaptop /> },
          { slug: "web-page-and-worker", title: "Web page and worker", content: <WebPageAndWorker /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <AgentsNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
