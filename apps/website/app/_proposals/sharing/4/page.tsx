import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ConnectOnce, ProposalNav } from "@/components/proposals";
import { ShareTry } from "@/components/share-try";

// How sharing works, proposal 4 - try it. The reader does the sharing: tap
// names, and the sentence and the answer write themselves. If it takes one tap
// here, the reader believes it takes one sentence there.

export const metadata = { title: "How sharing works · Proposal 4" };

export default function Page() {
  return (
    <PageLayoutContent width="medium" padding="normal" className="grid content-start gap-12 py-8">
      <ProposalNav set="sharing" current={4} />

      <Hero
        align="center"
        heading="Who should have it?"
        description="Pick them. That's what sharing is: you tell your agent, it does the rest."
      />

      <ShareTry />

      <ConnectOnce />
    </PageLayoutContent>
  );
}
