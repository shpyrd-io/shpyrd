import { Fragment, type ReactNode } from "react";
import { LandingPage } from "./landing";
import { editorialDesigns } from "@/lib/mdx/designs";
import type { EditorialLandingData, Inline, RichLanding } from "@/lib/mdx/types";

function inline(nodes?: Inline[]): ReactNode {
  return nodes?.map((node, index) => {
    const children = inline(node.children);
    switch (node.type) {
      case "text": return node.value;
      case "inlineCode": return <code key={index}>{node.value}</code>;
      case "strong": return <strong key={index}>{children}</strong>;
      case "emphasis": return <em key={index}>{children}</em>;
      case "delete": return <del key={index}>{children}</del>;
      case "link": return <a key={index} href={node.url} className="underline underline-offset-4">{children}</a>;
      case "break": return <br key={index} />;
      default: return <Fragment key={index}>{children}</Fragment>;
    }
  });
}

export function EditorialLanding({ page }: { page: EditorialLandingData }) {
  const design = editorialDesigns[page.slug];
  const steps = page.example.items.map((item, index) => ({
    ...item, icon: design?.steps[index] ?? "rocket" as const, body: inline(item.body),
  }));
  // The editorial compiler enforces the three steps of this existing layout.
  const content: RichLanding = {
    ...page,
    icon: design?.icon ?? "rocket",
    description: inline(page.description),
    example: { heading: page.example.heading, description: inline(page.example.description), steps: [steps[0], steps[1], steps[2]] },
    capabilities: {
      heading: page.capabilities.heading, description: inline(page.capabilities.description),
      items: page.capabilities.items.map((item, index) => ({ ...item, icon: design?.capabilities[index] ?? "code", body: inline(item.body) })),
    },
    questions: { heading: page.questions.heading, items: page.questions.items.map((item) => ({ q: item.heading, a: inline(item.body) })) },
    close: { ...page.close, description: inline(page.close.description) },
  };
  return <LandingPage page={content} />;
}
