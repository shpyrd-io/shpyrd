import { Check, Database, FolderUp, Globe, LockKeyhole, Moon, Sparkles } from "lucide-react";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";

// A proposal for the home's features (Giovani, 2026-10-16, after neon.com's
// "Build your backend"): one statement for a heading, centred, its first words
// in the colour of the text and the rest in grey; then five things in one row,
// lined up along their bottoms, each a
// name, one sentence and a small picture with no technical words in it. For
// people who built an app with AI and do not write code, not a catalogue.

// The small picture under each thing: a light card that draws what the
// sentence says, as an infographic, and acts it out under the pointer (the
// card is the group "item"). The brand's orange for what matters, green for
// what is fine, red for who is kept out.
function Thumb({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        glass,
        "relative mt-4 h-40 overflow-hidden rounded-2xl text-[11px] transition-[box-shadow,translate] duration-300 group-hover/item:-translate-y-1 group-hover/item:ring-primary/40 shadow-none dark:shadow-none",
        className,
      )}
    >
      {children}
    </div>
  );
}

const ease = "transition-all duration-700 ease-[cubic-bezier(0.22,1,0.36,1)]";

// Who gets in: the sign-in is a gate; a colleague walks through it, a
// stranger and a bot are stopped at it.
function Gate() {
  const lane = "absolute left-3 flex items-center gap-1.5";
  return (
    <>
      <div className="absolute inset-y-4 left-[62%] w-px bg-primary/50" />
      <div className="absolute top-2 left-[62%] grid size-7 -translate-x-1/2 place-items-center rounded-full bg-primary text-white shadow-[0_4px_12px_rgb(255_79_0/0.35)]">
        <LockKeyhole className="size-3.5" />
      </div>
      <div className={cn(lane, ease, "top-11 group-hover/item:translate-x-[7rem]")}>
        <span className="size-2 rounded-full bg-success" />
        <span className="rounded-full bg-background px-1.5 py-0.5 font-medium text-foreground ring-1 ring-foreground/10">you@company</span>
      </div>
      <div className={cn(lane, ease, "top-[4.75rem] delay-100 group-hover/item:translate-x-9")}>
        <span className="size-2 rounded-full bg-destructive" />
        <span className="rounded-full bg-background px-1.5 py-0.5 text-muted-foreground ring-1 ring-foreground/10">stranger</span>
      </div>
      <div className={cn(lane, ease, "top-[6.75rem] delay-200 group-hover/item:translate-x-14")}>
        <span className="size-2 rounded-full bg-muted-foreground/50" />
        <span className="rounded-full bg-background px-1.5 py-0.5 text-muted-foreground ring-1 ring-foreground/10">bot</span>
      </div>
      <span className="absolute right-3 bottom-3 font-medium text-destructive opacity-0 transition-opacity delay-500 duration-300 group-hover/item:opacity-100">
        2 turned away
      </span>
    </>
  );
}

// The database: the app on one side, its database on the other, records
// flowing in along the line between them.
function Store() {
  return (
    <>
      <div className="absolute top-1/2 left-4 -translate-y-1/2 rounded-md bg-background px-2 py-1 font-medium text-foreground ring-1 ring-foreground/10">
        your app
      </div>
      <div className="absolute top-1/2 right-[5.5rem] left-[4.75rem] h-px -translate-y-1/2 bg-[repeating-linear-gradient(90deg,var(--color-primary)_0_4px,transparent_4px_8px)] opacity-50" />
      {[0, 1, 2].map((i) => (
        <span
          key={i}
          className={cn(
            "absolute top-1/2 left-[4.75rem] size-1.5 -translate-y-1/2 rounded-full bg-primary opacity-0",
            "transition-[left,opacity] duration-1000 ease-in-out group-hover/item:left-[calc(100%_-_5.75rem)] group-hover/item:opacity-100",
          )}
          style={{ transitionDelay: `${i * 220}ms` }}
        />
      ))}
      <svg viewBox="0 0 48 56" className="absolute top-1/2 right-6 w-12 -translate-y-1/2 overflow-visible">
        <path d="M2 10 V46 C2 52 46 52 46 46 V10" className="fill-background stroke-primary" strokeWidth="1.5" />
        <ellipse cx="24" cy="10" rx="22" ry="7" className="fill-primary/15 stroke-primary" strokeWidth="1.5" />
        <path d="M2 22 C2 28 46 28 46 22 M2 34 C2 40 46 40 46 34" fill="none" className="stroke-primary/40" strokeWidth="1" />
      </svg>
      <div className="absolute right-3 bottom-2.5 grid text-right">
        <span className="col-start-1 row-start-1 font-medium text-muted-foreground transition-opacity duration-300 group-hover/item:opacity-0">
          12 contacts
        </span>
        <span className="col-start-1 row-start-1 font-medium text-foreground opacity-0 transition-opacity delay-700 duration-300 group-hover/item:opacity-100">
          128 contacts
        </span>
      </div>
    </>
  );
}

// Publishing: a folder on one side, the address on the other; a change
// travels across and the address lights up.
function Publish() {
  return (
    <>
      <div className="absolute top-1/2 left-4 grid -translate-y-1/2 justify-items-center gap-1">
        <FolderUp className="size-8 text-primary" strokeWidth={1.5} />
        <span className="font-medium text-foreground">my-app/</span>
      </div>
      <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="absolute top-1/2 right-[5.75rem] left-[4.5rem] h-10 -translate-y-1/2 overflow-visible">
        <path d="M0 20 C30 0 70 40 100 20" fill="none" className="stroke-primary/40" strokeWidth="1.5" strokeDasharray="3 4" vectorEffect="non-scaling-stroke" />
      </svg>
      <span className="absolute top-1/2 left-[4.5rem] size-2.5 -translate-y-1/2 rounded-sm bg-primary opacity-0 shadow-[0_0_10px_rgb(255_79_0/0.6)] transition-[left,opacity] duration-1000 ease-in-out group-hover/item:left-[calc(100%_-_6.25rem)] group-hover/item:opacity-100" />
      <div className="absolute top-1/2 right-3 grid -translate-y-1/2 justify-items-center gap-1">
        <div className="relative grid size-10 place-items-center rounded-full bg-background ring-1 ring-foreground/10">
          <Globe className="size-5 text-muted-foreground transition-colors delay-700 duration-300 group-hover/item:text-primary" strokeWidth={1.5} />
        </div>
        <span className="inline-flex items-center gap-1 font-medium text-foreground">
          <span className="size-1.5 rounded-full bg-muted-foreground/40 transition-colors delay-1000 duration-300 group-hover/item:bg-success" />
          yourapp.com
        </span>
      </div>
    </>
  );
}

// Sleeping: a day of use as bars; under the pointer the quiet hours fall to
// nothing and the moon comes up over them.
const day = [6, 9, 12, 14, 11, 13, 10, 7, 4, 3, 2, 2, 1, 1, 2, 5];
function Sleep() {
  return (
    <>
      <div className="absolute inset-x-4 bottom-7 flex h-20 items-end gap-[3px]">
        {day.map((h, i) => {
          const quiet = i >= 9 && i <= 14;
          return (
            <span
              key={i}
              className={cn(
                "flex-1 rounded-sm transition-[height,background-color] duration-700 ease-out",
                quiet ? "bg-primary/35 group-hover/item:!h-0.5 group-hover/item:bg-foreground/15" : "bg-primary",
              )}
              style={{ height: `${(h / 14) * 100}%`, transitionDelay: quiet ? `${(i - 9) * 70}ms` : "0ms" }}
            />
          );
        })}
      </div>
      <div className="absolute inset-x-4 bottom-2 flex justify-between text-muted-foreground">
        <span>9:00</span>
        <span>18:00</span>
        <span>6:00</span>
      </div>
      <div className="absolute top-3 right-3 flex translate-y-3 items-center gap-1 opacity-0 transition-[opacity,translate] delay-300 duration-500 group-hover/item:translate-y-0 group-hover/item:opacity-100">
        <Moon className="size-4 text-primary" />
        <span className="font-semibold text-foreground">asleep · $0</span>
      </div>
      <span className="absolute top-3 left-4 font-medium text-muted-foreground">Your app, one day</span>
    </>
  );
}

// Fixing: the app's speed over a day; an update makes it slow, and under the
// pointer the AI's undo brings it back.
function Fix() {
  return (
    <>
      <span className="absolute top-3 left-4 font-medium text-muted-foreground transition-opacity duration-300 group-hover/item:opacity-0">Speed of your app</span>
      <svg viewBox="0 0 200 100" preserveAspectRatio="none" className="absolute inset-x-4 top-8 bottom-6 h-auto w-[calc(100%_-_2rem)] overflow-visible">
        <line x1="80" y1="0" x2="80" y2="100" className="stroke-destructive/50" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
        <line x1="135" y1="0" x2="135" y2="100" className="stroke-success/0 transition-[stroke] delay-500 duration-300 group-hover/item:stroke-success/70" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
        {/* Slow after the update, and still slow. */}
        <path d="M0 80 L80 80 L84 25 L200 28" fill="none" className="stroke-destructive transition-opacity duration-500 group-hover/item:opacity-0" strokeWidth="2" vectorEffect="non-scaling-stroke" />
        {/* Slow after the update, then back once undone. */}
        <path
          d="M0 80 L80 80 L84 25 L135 28 L139 80 L200 80"
          fill="none"
          pathLength={1}
          className="stroke-primary opacity-0 transition-[opacity,stroke-dashoffset] duration-1000 [stroke-dasharray:1] [stroke-dashoffset:1] group-hover/item:opacity-100 group-hover/item:[stroke-dashoffset:0]"
          strokeWidth="2"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <span className="absolute bottom-2 left-[calc(1rem_+_38%)] -translate-x-1/2 font-medium text-destructive">update</span>
      <span className="absolute top-3 right-3 inline-flex items-center gap-1 font-semibold text-success opacity-0 transition-opacity delay-700 duration-300 group-hover/item:opacity-100">
        <Check className="size-3" /> undone by your AI
      </span>
    </>
  );
}

const items: { icon: React.ReactElement; name: string; line: string; picture: React.ReactNode }[] = [
  {
    icon: <LockKeyhole />,
    name: "Only your people get in.",
    line: "One click puts a sign-in in front of your app, no login code.",
    picture: <Gate />,
  },
  {
    icon: <Database />,
    name: "A database, ready to use.",
    line: "Your data is saved next to your app. Nothing to set up.",
    picture: <Store />,
  },
  {
    icon: <FolderUp />,
    name: "Publish from your folder.",
    line: "Or from GitHub. Every change you save goes online by itself.",
    picture: <Publish />,
  },
  {
    icon: <Moon />,
    name: "Free while nobody uses it.",
    line: "Your app sleeps when it is quiet, and wakes in seconds.",
    picture: <Sleep />,
  },
  {
    icon: <Sparkles />,
    name: "Your AI keeps it running.",
    line: "Something broke? Ask. It finds the cause and undoes the update.",
    picture: <Fix />,
  },
];

export function GoLive() {
  return (
    <section className="grid gap-12">
      <h2 className="mx-auto max-w-[34ch] text-center font-heading text-4xl leading-tight font-semibold tracking-tight text-balance sm:text-5xl">
        Everything your app needs.{" "}
        <span className="text-muted-foreground">
          A sign-in, a database, an address and a way back when it breaks. Nothing for you to set up.
        </span>
      </h2>

      {/* The pictures line up along their bottoms, whatever the length of the words over them. */}
      <ul className="grid items-end gap-x-5 gap-y-10 sm:grid-cols-2 lg:grid-cols-5">
        {items.map((item) => (
          <li key={item.name} className="group/item grid content-end">
            {/* Three lines each, so the five read as one row of equal blocks. */}
            <p className="min-h-[calc(3lh_+_1.75rem)] text-sm text-muted-foreground">
              <span className="mb-2 block text-primary [&_svg]:size-5">{item.icon}</span>
              <span className="font-semibold text-foreground">{item.name}</span> {item.line}
            </p>
            <Thumb>{item.picture}</Thumb>
          </li>
        ))}
      </ul>
    </section>
  );
}
