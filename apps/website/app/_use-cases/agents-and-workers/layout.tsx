import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionTabs } from "@/components/section-tabs";

// Agents and workers: its subpages, with the bar over them that stays while they change.
// The first is the section's own page.
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <PageLayoutContent as="div">
      <div className="mx-auto w-full max-w-[67.5rem] px-4 pt-8 @3xl/page-layout:px-6">
        <SectionTabs
          base="/use-cases/agents-and-workers"
          label="Agents and workers"
          pages={[
            { title: "Overview", slug: "" },
            { title: "Move it off your laptop", slug: "move-it-off-your-laptop" },
            { title: "Web page and worker", slug: "web-page-and-worker" },
            { title: "Why not your laptop", slug: "why-not-your-laptop" },
          ]}
        />
      </div>
      {children}
    </PageLayoutContent>
  );
}
