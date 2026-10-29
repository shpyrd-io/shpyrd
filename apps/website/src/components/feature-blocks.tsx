import { Activity, Bot, Database, GitBranch, Gauge, LayoutDashboard, LockKeyhole, Moon } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { AgentChatFilm } from "@/components/agent-chat-film";
import { DoorFilm } from "@/components/door-film";
import { ResourcesFilm } from "@/components/resources-film";
import { PublishFilm } from "@/components/publish-film";
import { SleepFilm } from "@/components/sleep-film";
import { CostFilm } from "@/components/cost-film";
import { MetricsFilm } from "@/components/metrics-film";
import { OverviewFilm } from "@/components/overview-film";

// Eight things shpyrd does, each a statement and a little film of it playing
// (first from the designer's sheet, claude.ai/artifact/R8pQc5eZnfonk2iQgg6CpP;
// redrawn 2026-10-19). Each sits in the glass of the menu; the films are
// React (src/components/*-film.tsx, on film.tsx), in greys and still until the
// pointer is on their block (src/styles/feature-blocks.css).

// The name of a block, after supabase.com's: an icon and a few words over
// the line that says what it does.
function Heading({ icon, title }: { icon: React.ReactElement; title: string }) {
  return (
    <h3 className="flex items-center gap-2 text-base font-semibold text-foreground [&_svg]:size-[18px] [&_svg]:shrink-0 [&_svg]:text-primary">
      {icon}
      {title}
    </h3>
  );
}

// One card of the carousel (inside .fb, where --muted is the films' grey
// ink, so the card's greys are drawn from the foreground instead): the film
// or the picture on top, in its own light
// window; under it the feature's name as a chip, the statement in large type
// and its line. Under the pointer the card rises, its shadow deepens, its
// edge turns orange and the picture comes a little closer.
function Card({
  icon,
  chip,
  title,
  body,
  children,
}: {
  icon: React.ReactElement;
  chip: string;
  title: string;
  body: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <article
      className={cn(
        glass,
        "group/card flex w-[19rem] shrink-0 snap-start flex-col overflow-hidden p-0 transition-[box-shadow] duration-300 ease-out sm:w-[22rem]",
        "shadow-none hover:ring-primary/50 dark:shadow-none dark:hover:ring-primary/50",
      )}
    >
      {/* The film: square, on the card itself, with no line, ground or
          grid of its own between it and the words (Giovani, 2026-10-19). */}
      <div className="relative aspect-square overflow-hidden [&>div>.stage]:bg-none!">
        <div className="absolute inset-0">{children}</div>
      </div>
      <div className="grid content-start gap-3 p-6">
        <span className="inline-flex w-fit items-center gap-1.5 rounded-md bg-foreground/[0.06] px-2 py-1 text-xs font-medium text-foreground [&_svg]:size-3.5 [&_svg]:text-primary">
          {icon}
          {chip}
        </span>
        <h3 className="font-heading text-xl leading-snug font-semibold tracking-tight text-foreground">{title}</h3>
        <p className="text-sm text-muted-foreground">{body}</p>
      </div>
    </article>
  );
}

// One tile of the board, after supabase.com's product grid (Giovani,
// 2026-10-19): a thin edge, the name and its line at the top, the film
// filling the rest of the tile to its edges, with no frame of its own. The
// board's first and last tiles lay the words beside the film
// (src/styles/feature-blocks.css). The films play while the pointer is on the
// tile (src/components/film.tsx).
function Tile({
  icon,
  title,
  body,
  stageClass,
  children,
}: {
  icon: React.ReactElement;
  title: string;
  body: React.ReactNode;
  stageClass?: string;
  children: React.ReactNode;
}) {
  return (
    <article
      data-tile=""
      className="group/tile relative flex flex-col overflow-hidden rounded-2xl border border-foreground/8 bg-background/70 transition-colors duration-300 hover:border-primary/50 dark:bg-card/40"
    >
      <div className="grid content-start gap-2 p-6">
        <Heading icon={icon} title={title} />
        <p className="text-sm text-muted-foreground">{body}</p>
      </div>
      <div aria-hidden="true" className={cn("stage relative min-h-0 flex-1 rounded-none border-0 bg-none!", stageClass)}>
        {children}
      </div>
    </article>
  );
}

// A statement and its film. `row` puts the film beside the words, for a
// block that spans more columns than it is tall.
function Block({
  icon,
  title,
  chip,
  carousel = false,
  children,
  stage,
  stageClass,
  className,
  row = false,
  board = false,
}: {
  icon: React.ReactElement;
  title: string;
  chip: string;
  carousel?: boolean;
  children: React.ReactNode;
  stage: React.ReactNode;
  stageClass?: string;
  className?: string;
  row?: boolean;
  board?: boolean;
}) {
  if (board) {
    return (
      <Tile icon={icon} title={title} body={children} stageClass={stageClass}>
        {stage}
      </Tile>
    );
  }
  if (carousel) {
    return (
      <Card icon={icon} chip={chip} title={title} body={children}>
        <div aria-hidden="true" className={cn("stage size-full rounded-none border-0 bg-transparent", stageClass)}>
          {stage}
        </div>
      </Card>
    );
  }
  return (
    // No shadow (Giovani, 2026-10-19): under the pointer only its edge turns
    // orange.
    <article
      data-tile=""
      className={cn(
        glass,
        "flex flex-col gap-4 p-2.5 transition-[box-shadow] duration-300 ease-out",
        "shadow-none hover:ring-primary/50 dark:shadow-none dark:hover:ring-primary/50",
        row && "md:flex-row md:items-stretch",
        className,
      )}
    >
      <div className={cn("grid gap-2 px-3.5 pt-3.5", row && "md:max-w-[34%] md:pb-3.5 md:self-start")}>
        <Heading icon={icon} title={title} />
        <p className="text-sm text-muted-foreground">{children}</p>
      </div>
      <div
        aria-hidden="true"
        className={cn(
          // No checkered grid behind the films (Giovani, 2026-10-19).
          "stage mt-auto flex-1 rounded-2xl border border-white/80 bg-none! dark:border-white/10",
          row && "md:mt-0",
          stageClass,
        )}
      >
        {stage}
      </div>
    </article>
  );
}

// `carousel` lays the same blocks out as a row of cards that scrolls
// sideways, after stripe.com/br/payments: the film on top, then the name of
// the feature as a chip, the statement and its line (kept as a model,
// /proposals/carousel). `grid` sets the same cards in rows of four, two rows
// for the eight (a model too). `board` lays them out as supabase.com's
// product grid (a model too).
//
// The order and the shapes (Giovani, 2026-10-19): each film where its
// drawing fits, in the order of the story. Publishing first, tall, its cloud
// over the folder and GitHub; beside it what it costs, wide, a chart; under
// that who gets in and the database. Then the health chart, wide, and the AI
// keeping it running, tall, the whole conversation in view; last, sleep and
// the app's page.
export function FeatureBlocks({
  carousel = false,
  grid = false,
  board = false,
  className,
}: {
  carousel?: boolean;
  grid?: boolean;
  // The tiles of supabase.com's product grid: a wide one and two narrow, then
  // four, then the last across (the home, 2026-10-19).
  board?: boolean;
  className?: string;
}) {
  const cards = carousel || grid;
  return (
    <div
      className={cn(
        "fb",
        board
          ? "board"
          : carousel
          ? "flex snap-x snap-mandatory scroll-px-1 gap-5 overflow-x-auto px-1 pt-2 pb-8 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
          : grid
            ? "grid gap-5 pt-2 sm:grid-cols-2 lg:grid-cols-4 [&>article]:w-auto"
            : "mt-8 grid grid-flow-dense gap-4 md:grid-cols-6",
        className,
      )}
    >
      <Block
        icon={<GitBranch />}
        title="Publish from your folder or GitHub"
        chip="Deploy from Git or a folder"
        carousel={cards}
        board={board}
        className="md:col-span-2 md:row-span-2"
        stageClass="s-publish"
        stage={<PublishFilm />}
      >
        Send the folder you have, or connect a GitHub repository. From GitHub, each change you push goes online by itself.
      </Block>

      <Block
        icon={<Gauge />}
        title="Pay only for what it uses"
        chip="Billed by use"
        carousel={cards}
        board={board}
        row
        className="md:col-span-4"
        stageClass="s-cost"
        stage={<CostFilm />}
      >
        Apps start small and you pay for the work they actually do, so an agent waiting on the AI costs almost nothing.
      </Block>

      <Block
        icon={<LockKeyhole />}
        title="Only your people get in"
        chip="Security Door"
        carousel={cards}
        board={board}
        className="md:col-span-2"
        stageClass="s-door"
        stage={<DoorFilm />}
      >
        One click puts a sign-in in front of your app. You choose who opens it, and you write no login code.
      </Block>

      <Block
        icon={<Database />}
        title="A database, ready to use"
        chip="Database, cache and disks"
        carousel={cards}
        board={board}
        className="md:col-span-2"
        stageClass="s-res"
        stage={<ResourcesFilm />}
      >
        Your app gets a database and room for its files, next to it. Your agent connects them; there is nothing to set up.
      </Block>

      <Block
        icon={<Activity />}
        title="See that it is healthy"
        chip="Metrics and logs"
        carousel={cards}
        board={board}
        row
        className="md:col-span-4"
        stageClass="s-met"
        stage={<MetricsFilm />}
      >
        Visits, speed and errors on one chart, with every update marked, so you see what changed when something goes wrong.
      </Block>

      <Block
        icon={<Bot />}
        title="Your AI keeps it running"
        chip="Agent operations"
        carousel={cards}
        board={board}
        className="md:col-span-2 md:row-span-2"
        stageClass="s-agent"
        stage={<AgentChatFilm />}
      >
        Ask Claude Code, Codex or Cursor why something is slow or broken. It reads the logs, finds the cause and undoes a bad update for you.
      </Block>

      <Block
        icon={<Moon />}
        title="Free while nobody uses it"
        chip="Sleep"
        carousel={cards}
        board={board}
        className="md:col-span-2"
        stageClass="s-sleep"
        stage={<SleepFilm />}
      >
        When nobody uses your app it sleeps and costs nothing to run. The next person who signs in wakes it in seconds; bots never do.
      </Block>

      <Block
        icon={<LayoutDashboard />}
        title="Everything about it, on one page"
        chip="Project overview"
        carousel={cards}
        board={board}
        className="md:col-span-2"
        stageClass="s-overview"
        stage={<OverviewFilm />}
      >
        Which version is online, what it uses, who can change it and what they did.
      </Block>
    </div>
  );
}
