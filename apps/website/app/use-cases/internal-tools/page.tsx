import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { ConnectOnce } from "@/components/proposals";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as ToolsPeopleBuild } from "./_parts/tools-people-build";
import { Part as WhoDoesWhat } from "./_parts/who-does-what";
import { Part as OneWeekOneTracker } from "./_parts/one-week-one-tracker";

// Internal tools, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "Internal tools",
  description:
    "The tracker, the dashboard, the approval tool you built with AI. Tell your agent who it's for, and it's in their list of apps tomorrow morning.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="Internal tools"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "tools-people-build", title: "Tools people build", content: <ToolsPeopleBuild /> },
          { slug: "who-does-what", title: "Who does what", content: <WhoDoesWhat /> },
          { slug: "one-week-one-tracker", title: "One week, one tracker", content: <OneWeekOneTracker /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <ConnectOnce />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
