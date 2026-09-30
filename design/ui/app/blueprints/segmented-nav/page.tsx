"use client";

import { useState } from "react";
import { SegmentedNav } from "@shpyrd/ui/components/segmented-nav";
import { Section } from "../../section";

const pages = ["The loop", "Try it first", "Set it up once", "Nothing hidden"];

// The gallery has no router: a click here only moves the light, which is
// what a page change does to a bar that lives in the section's layout.
function Demo({
  variant,
  align,
  names = pages,
  className,
}: {
  variant?: "pill" | "underline";
  align?: "start" | "center";
  names?: string[];
  className?: string;
}) {
  const [open, setOpen] = useState(0);
  return (
    <SegmentedNav
      aria-label="For developers"
      variant={variant}
      align={align}
      className={className}
      links={names.map((name, i) => (
        <a
          key={name}
          href={`#${i}`}
          aria-current={i === open ? "page" : undefined}
          onClick={(event) => {
            event.preventDefault();
            setOpen(i);
          }}
        >
          {name}
        </a>
      ))}
    />
  );
}

export default function Page() {
  return (
    <>
      <Section title="The pages of a section; the light slides to the one that is open">
        <Demo />
      </Section>

      <Section title="At the start of its room">
        <Demo align="start" />
      </Section>

      <Section title="A line under the open page">
        <Demo variant="underline" align="start" />
      </Section>

      <Section title="In a narrow room it scrolls, and keeps the open page in view">
        <div className="max-w-xs rounded-xl border p-3">
          <Demo
            names={["Into their day", "Find yours", "Yours and ours", "One tracker", "Keep the few", "Monday morning"]}
          />
        </div>
      </Section>
    </>
  );
}
