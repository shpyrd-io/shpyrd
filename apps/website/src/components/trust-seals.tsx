// What lowers the risk of trying shpyrd, as seals of the awards' kind, and
// under them the awards themselves (Giovani, 2026-10-29, on the manifesto's
// last slide; the home's hero too since 2026-11-11). Each seal as high as
// the badges, white, with the badges' own edge (#e5e5e5, the ink at 12%) as
// fine as theirs: their 1px is drawn at 54px and shown at 36px, so two
// thirds of a pixel (Giovani, 2026-11-11: the seals read heavier); its icon
// in orange and a small line over its words; 10px between them every way.
import { Cloud, Code2, FolderGit2 } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { devHunt, peerPush, productHunt } from "@/content/home";

const seals = [
  { icon: <FolderGit2 />, over: "From your repository", name: "No rewrites" },
  {
    icon: <Cloud />,
    over: "Runs where you choose",
    name: "Your cloud or ours",
  },
  { icon: <Code2 />, over: "The code is public", name: "Open source" },
];

export function TrustSeals({
  className,
  rowClassName,
  rowStyle,
  lit = false,
}: {
  className?: string;
  // The light of the manifesto's last slide on them (Giovani, 2026-11-11):
  // there the CTA block's orange aurora falls over the seals and tints them;
  // here a still wash of it, multiplied on the light page and screened on
  // the dark, as the aurora is.
  lit?: boolean;
  // Each row's own classes and style: the seals' (0), the awards' (1).
  rowClassName?: string;
  rowStyle?: (row: number) => React.CSSProperties | undefined;
}) {
  return (
    <span className={cn("grid gap-2.5", lit && "relative", className)}>
      {lit && (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute -inset-8 z-10 bg-[radial-gradient(closest-side,rgb(255_79_0/0.11),rgb(255_122_61/0.08)_55%,transparent)] mix-blend-multiply dark:bg-[radial-gradient(closest-side,rgb(255_79_0/0.26),rgb(255_122_61/0.16)_55%,transparent)] dark:mix-blend-screen"
        />
      )}
      <span
        className={cn("flex flex-wrap items-center gap-2.5", rowClassName)}
        style={rowStyle?.(0)}
      >
        {seals.map((x) => (
          <span
            key={x.name}
            className="inline-flex h-9 shrink-0 items-center gap-2 rounded-lg bg-background px-2.5 ring-[0.667px] ring-foreground/12 dark:bg-card [&_svg]:size-5 [&_svg]:text-primary"
          >
            {x.icon}
            <span className="grid text-left leading-none">
              <span className="text-[8px] font-semibold tracking-wider text-muted-foreground uppercase">
                {x.over}
              </span>
              <span className="mt-0.5 text-[13px] font-semibold text-foreground">
                {x.name}
              </span>
            </span>
          </span>
        ))}
      </span>
      {/* The awards: proof that others have tried it, kept quiet. */}
      <span
        className={cn("flex flex-wrap items-center gap-2.5", rowClassName)}
        style={rowStyle?.(1)}
      >
        <a
          href={productHunt.href}
          target="_blank"
          rel="noopener noreferrer"
          className="shrink-0"
        >
          {/* The badge in the site's theme, a light one and a dark one: Product Hunt's own, kept in public/badges without the upvote count until the launch. */}
          {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
          <img
            alt={productHunt.alt}
            width={202}
            height={54}
            src={productHunt.image}
            className="h-9 w-auto dark:hidden"
          />
          {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
          <img
            alt={productHunt.alt}
            width={202}
            height={54}
            src={productHunt.imageDark}
            className="hidden h-9 w-auto dark:block"
          />
        </a>
        <a
          href={peerPush.href}
          target="_blank"
          rel="noopener noreferrer"
          className="shrink-0"
        >
          {/* eslint-disable-next-line @next/next/no-img-element -- PeerPush serves the badge */}
          <img
            src={peerPush.image}
            alt={peerPush.alt}
            width={191}
            height={54}
            className="h-9 w-auto"
          />
        </a>
        <a
          href={devHunt.href}
          target="_blank"
          rel="noopener noreferrer"
          title="shpyrd on DevHunt"
          className="shrink-0"
        >
          {/* eslint-disable-next-line @next/next/no-img-element -- DevHunt serves the badge */}
          <img
            src={devHunt.image}
            alt={devHunt.alt}
            width={220}
            height={54}
            className="h-9 w-auto"
          />
        </a>
      </span>
    </span>
  );
}
