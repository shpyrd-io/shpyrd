import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { DeveloperNext } from "@/components/for-developers";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as YourFirstDeploy } from "./_parts/your-first-deploy";
import { Part as WhatsIncluded } from "./_parts/whats-included";
import { Part as RunItYourself } from "./_parts/run-it-yourself";

// For developers, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "For developers",
  description:
    "Deploy from your folder, get a URL with TLS, roll back in one command. On shpyrd cloud, with nothing to install or run.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="For developers"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "your-first-deploy", title: "Your first deploy", content: <YourFirstDeploy /> },
          { slug: "whats-included", title: "What's included", content: <WhatsIncluded /> },
          { slug: "run-it-yourself", title: "Run it yourself", content: <RunItYourself /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <DeveloperNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
