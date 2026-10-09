import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { PartnerNext } from "@/components/for-fde-partners";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as DeliveringAnApp } from "./_parts/delivering-an-app";
import { Part as HandingItOver } from "./_parts/handing-it-over";
import { Part as InTheClientsCloud } from "./_parts/in-the-clients-cloud";

// For FDE partners, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = { title: "For FDE partners" };

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="For FDE partners"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "delivering-an-app", title: "Delivering an app", content: <DeliveringAnApp /> },
          { slug: "handing-it-over", title: "Handing it over", content: <HandingItOver /> },
          { slug: "in-the-clients-cloud", title: "In the client's cloud", content: <InTheClientsCloud /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <PartnerNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
