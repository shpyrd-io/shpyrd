import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { ClientAppsNext } from "@/components/use-case-client-apps";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as FromDemoToLaunch } from "./_parts/from-demo-to-launch";
import { Part as TellYourAgent } from "./_parts/tell-your-agent";
import { Part as TheDeliverySheet } from "./_parts/the-delivery-sheet";

// Client apps, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "Client apps",
  description:
    "The app you built for a client, opened by the client's people with the account they already have, in a workspace on shpyrd cloud or in the client's own cloud.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="Client apps"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "from-demo-to-launch", title: "From demo to launch", content: <FromDemoToLaunch /> },
          { slug: "tell-your-agent", title: "Tell your agent", content: <TellYourAgent /> },
          { slug: "the-delivery-sheet", title: "The delivery sheet", content: <TheDeliverySheet /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <ClientAppsNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
