import { ChecklistItems } from "@shpyrd/ui/components/checklist";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContactForm } from "@/components/contact-form";
import { formOf, type Kind } from "@/lib/contact";

// A contact page: its heading, the form, and beside it what the call covers
// and what follows.
export function Contact({ kind }: { kind: Kind }) {
  const page = formOf(kind);
  return (
    <PageLayoutContent width="xlarge" padding="normal" className="grid grid-cols-1 content-start gap-12 py-8">
      <Hero variant="page" heading={page.title} description={page.description} />
      <div className="grid gap-12 @4xl/page-layout:grid-cols-[3fr_2fr]">
        <ContactForm kind={kind} />
        <aside className="grid content-start gap-8">
          {[page.beside, page.after].map((part) => (
            <section key={part.heading} className="grid gap-3">
              <h2 className="font-medium">{part.heading}</h2>
              <ChecklistItems items={part.items} />
            </section>
          ))}
        </aside>
      </div>
    </PageLayoutContent>
  );
}
