import type { ReactNode } from "react";
import type { Icon, Landing } from "@/content/landings";

export type FeatureEntry = { slug: string; name: string; card: string; icon: Icon };
export type Frontmatter = {
  title: string;
  description: string;
  layout?: "landing" | "copy";
  date?: string;
  type?: "feature" | "improvement" | "fix";
  draft?: boolean;
  menu?: { label: string; description: string; order: number; group?: string };
};
export type Inline = {
  type: "text" | "strong" | "emphasis" | "delete" | "inlineCode" | "link" | "break";
  value?: string;
  url?: string;
  children?: Inline[];
};
type RichItem = { icon: Icon; heading: string; body: ReactNode };
export type RichLanding = Omit<Landing, "description" | "example" | "capabilities" | "questions" | "close"> & {
  description: ReactNode;
  example: { heading: string; description?: ReactNode; steps: [RichItem, RichItem, RichItem] };
  capabilities: { heading: string; description?: ReactNode; items: RichItem[] };
  questions: { heading: string; items: { q: string; a: ReactNode }[] };
  close: { heading: string; description: ReactNode; action: Landing["close"]["action"] };
};
type EditorialSection = { heading: string; description?: Inline[]; items: { heading: string; body: Inline[] }[] };
export type EditorialLandingData = Pick<Landing, "slug" | "name" | "label" | "card" | "heading" | "primary" | "secondary" | "related"> & {
  description: Inline[];
  example: EditorialSection;
  capabilities: EditorialSection;
  questions: EditorialSection;
  close: { heading: string; description: Inline[]; action: Landing["close"]["action"] };
};
