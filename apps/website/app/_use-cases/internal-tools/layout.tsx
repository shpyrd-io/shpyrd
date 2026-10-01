import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// Internal tools: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-[67.5rem] px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/use-cases/internal-tools"
          label="Internal tools"
          pages={[
            { title: "Overview", slug: "" },
            { title: "Tools people build", slug: "tools-people-build" },
            { title: "Who does what", slug: "who-does-what" },
            { title: "One week, one tracker", slug: "one-week-one-tracker" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
