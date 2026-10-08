import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// For developers: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-7xl px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/for/developers"
          label="For developers"
          pages={[
            { title: "Overview", slug: "" },
            { title: "Your first deploy", slug: "your-first-deploy" },
            { title: "What's included", slug: "whats-included" },
            { title: "Run it yourself", slug: "run-it-yourself" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
