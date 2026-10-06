import { LogoMark, Wordmark, type BrandSurface } from "@shpyrd/ui/components/brand";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

// What the logo sits on, and the colours it takes there.
const surfaces: { surface: Exclude<BrandSurface, "auto">; label: string; background: string }[] = [
  { surface: "dark", label: "On dark: the symbol orange, the name white", background: "bg-black" },
  { surface: "orange", label: "On orange: the symbol white, the name black", background: "bg-[#ff4f00]" },
  { surface: "light", label: "On white: the symbol orange, the name black", background: "bg-white border" },
];

export default function Page() {
  return (
    <>
      <Section title="Wordmark">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Wordmark />
          <Wordmark className="h-10" />
        </Stack>
      </Section>
      <Section title="Mark">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <LogoMark />
          <LogoMark className="size-10" />
          <LogoMark className="size-16" />
        </Stack>
      </Section>
      <Section title="On each background">
        <div className="grid gap-3 sm:grid-cols-3">
          {surfaces.map(({ surface, label, background }) => (
            <figure key={surface} className="grid gap-2">
              <div className={`flex h-32 items-center justify-center gap-6 rounded-lg ${background}`}>
                <Wordmark surface={surface} className="h-10" />
                <LogoMark surface={surface} className="size-10" />
              </div>
              <figcaption className="text-xs text-muted-foreground">{label}</figcaption>
            </figure>
          ))}
        </div>
      </Section>
    </>
  );
}
