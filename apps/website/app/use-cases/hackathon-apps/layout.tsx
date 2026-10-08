import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// Apps from a hackathon: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-7xl px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/use-cases/hackathon-apps"
          label="Apps from a hackathon"
          pages={[
            { title: "Overview", slug: "" },
            { title: "After the demo", slug: "after-the-demo" },
            { title: "Rollout checklist", slug: "rollout-checklist" },
            { title: "For organisers", slug: "for-organisers" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
