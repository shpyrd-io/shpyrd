import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";

// The simple CTA (Giovani, 2026-10-08): the end of a page that does not need
// the shipyard. Its words on the CTA block's light ("brilho na água",
// src/styles/aurora.css), centred on them; the one thing to do in the middle,
// in orange and large; where to read on, as plain links further down, near
// the footer (48px from its line, as a subpage's ending is).
export function SimpleCta({
  heading,
  description,
  action,
  links = [],
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  // The one thing to do: a large orange button (or the "Add to" button).
  action: React.ReactNode;
  // Where else to go, quieter.
  links?: { label: string; href: string }[];
}) {
  return (
    <div data-slot="next-step" className="relative isolate grid justify-items-center gap-24 overflow-x-clip pb-6">
      {/* The light under the words and the button, not the links. */}
      <Stack gap="spacious" className="relative isolate w-full justify-items-center">
        <div aria-hidden="true" className="aurora aurora-centred">
          <span className="light" />
          <span className="light" />
          <span className="light" />
          <span className="light" />
          <span className="rays" />
        </div>
        <SectionIntro
          align="center"
          variant="xlarge"
          className="w-full [&_[data-slot=section-intro-description]]:text-muted-foreground dark:[&_[data-slot=section-intro-description]]:text-foreground/85"
          heading={heading}
          description={description}
        />
        <div className="mt-8 flex w-full justify-center [&>*]:items-center">{action}</div>
      </Stack>
      {links.length > 0 && (
        <nav aria-label="Read on" className="flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm">
          {links.map((l) => (
            <a
              key={l.href}
              href={l.href}
              className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            >
              {l.label}
            </a>
          ))}
        </nav>
      )}
    </div>
  );
}
