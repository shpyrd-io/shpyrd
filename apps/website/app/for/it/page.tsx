import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContinuousSection } from "@/components/continuous-section";
import { ItNext } from "@/components/for-it";
import { pageSections } from "@/lib/page";
import { Part as Overview } from "./_parts/overview";
import { Part as SignInAndAccess } from "./_parts/sign-in-and-access";
import { Part as BeforeYouApprove } from "./_parts/before-you-approve";
import { Part as CommonQuestions } from "./_parts/common-questions";

// For IT teams, read as one page (continuous-section.tsx): its parts one
// after another under a bar that stays, and one ending.
export const metadata = {
  title: "For IT teams",
  description:
    "Give the apps your people build one accepted place to run: on shpyrd cloud, behind your company's sign-in, with you deciding who uses and changes each one.",
};

export default function Page() {
  return (
    <PageLayoutContent as="div">
      <ContinuousSection
        label="For IT teams"
        parts={[
          { slug: "overview", title: "Overview", content: <Overview /> },
          { slug: "sign-in-and-access", title: "Sign-in and access", content: <SignInAndAccess /> },
          { slug: "before-you-approve", title: "Before you approve", content: <BeforeYouApprove /> },
          { slug: "common-questions", title: "Common questions", content: <CommonQuestions /> },
        ]}
        ending={
          <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
            <ItNext />
          </PageLayoutContent>
        }
      />
    </PageLayoutContent>
  );
}
