import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// For IT teams: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-7xl px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/for/it"
          label="For IT teams"
          pages={[
            { title: "Overview", slug: "" },
            { title: "Sign-in and access", slug: "sign-in-and-access" },
            { title: "Common questions", slug: "common-questions" },
            { title: "Before you approve", slug: "before-you-approve" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
