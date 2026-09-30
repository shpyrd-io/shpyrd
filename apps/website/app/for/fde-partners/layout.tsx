import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// For FDE partners: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-[67.5rem] px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/for/fde-partners"
          label="For FDE partners"
          pages={[
            { title: "Overview", slug: "" },
            { title: "Delivering an app", slug: "delivering-an-app" },
            { title: "Handing it over", slug: "handing-it-over" },
            { title: "In the client's cloud", slug: "in-the-clients-cloud" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
