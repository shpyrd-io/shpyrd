import {
  PageLayout,
  PageLayoutContent,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutPane,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { Section } from "../../section";

// The layouts here are inside a page that is itself a layout, so their
// content is a `div`: a page has one `main`.
export default function Page() {
  return (
    <>
      <Section title="Default">
        <Frame>
          <PageLayout>
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutPane>
              <Area className="h-32">Pane</Area>
            </PageLayoutPane>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="Lines between the areas">
        <Frame>
          <PageLayout>
            <PageLayoutHeader divider="line" padding="condensed">
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutPane divider="line" padding="condensed">
              <Area className="h-32">Pane</Area>
            </PageLayoutPane>
            <PageLayoutFooter divider="line" padding="condensed">
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="No room around, no gaps">
        <Frame>
          <PageLayout padding="none" rowGap="none" columnGap="none">
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutPane width="small">
              <Area className="h-32">Pane</Area>
            </PageLayoutPane>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="The pane at the start">
        <Frame>
          <PageLayout>
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutPane position="start" width="small">
              <Area className="h-32">Pane</Area>
            </PageLayoutPane>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="A sidebar, from top to bottom">
        <Frame>
          <PageLayout>
            <PageLayoutSidebar width="small">
              <Area className="h-full min-h-32">Sidebar</Area>
            </PageLayoutSidebar>
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="A sidebar at the end, and a pane">
        <Frame>
          <PageLayout>
            <PageLayoutSidebar position="end" width="small" divider="line" padding="condensed">
              <Area className="h-full min-h-32">Sidebar</Area>
            </PageLayoutSidebar>
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-64">Content</Area>
            </PageLayoutContent>
            <PageLayoutPane position="start" width="small">
              <Area className="h-32">Pane</Area>
            </PageLayoutPane>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>

      <Section title="With less than 768 pixels of room, one under the other">
        <Frame className="max-w-sm">
          <PageLayout>
            <PageLayoutHeader>
              <Area className="h-16">Header</Area>
            </PageLayoutHeader>
            <PageLayoutContent as="div">
              <Area className="h-40">Content</Area>
            </PageLayoutContent>
            <PageLayoutPane>
              <Area className="h-24">Pane</Area>
            </PageLayoutPane>
            <PageLayoutFooter>
              <Area className="h-16">Footer</Area>
            </PageLayoutFooter>
          </PageLayout>
        </Frame>
      </Section>
    </>
  );
}

// A layout is for a whole page and the gallery has less room than that,
// so each one is drawn smaller: it has the room of a page, at 70%.
function Frame({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <div className={`overflow-hidden rounded-xl ring-1 ring-foreground/10 ${className ?? ""}`}>
      <div className="[zoom:0.7]">{children}</div>
    </div>
  );
}

function Area({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <div
      className={`grid place-items-center rounded-lg border bg-muted/50 text-sm text-muted-foreground ${className ?? ""}`}
    >
      {children}
    </div>
  );
}
