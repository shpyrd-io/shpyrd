import { FileCode } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { workerYaml } from "@/components/use-case-agents-and-workers";
import { CodeWindow } from "@/components/code-window";
import { pageSections } from "@/lib/page";

// Agents and workers, the section's own page: what you built with AI doesn't
// need a web page to be run properly, what "properly" means, and the one thing
// that isn't there yet (a schedule). Its subpages each take one part further.


export function Part() {
  return (
    <>
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero variant="page"
        label="Agents and workers"
        heading="Not everything you build has a web page."
        description="The agent that reads your inbox. The worker that turns uploads into reports. The bot in your team chat. shpyrd runs them too, on shpyrd cloud, the same way it runs apps."
        image={<CodeWindow title="shpyrd.yaml" icon={<FileCode />} code={workerYaml} language="yaml" roomy />}
      />



    </PageLayoutContent>
    </>
  );
}
