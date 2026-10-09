import { Activity, Bot, Database, GitBranch, Gauge, LayoutDashboard, LockKeyhole, Moon } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

// Six things shpyrd does, each a statement and a little film of it playing,
// from the designer's sheet (claude.ai/artifact/R8pQc5eZnfonk2iQgg6CpP). Each
// sits in the glass of the menu, its film in a light window like the hero's
// chat. The films are CSS (src/styles/feature-blocks.css), drawn here once.

// Metrics: p50, p95 and p99 over a day, a spike after v42 that the rollback ends.
function series(normal: number, spike: number, noise: number, seed: number) {
  const pts: string[] = [];
  for (let i = 0; i <= 44; i++) {
    const x = 34 + i * 8;
    const bad = x > 170 && x < 290;
    let v = bad ? spike : normal;
    v += (bad ? noise * 3 : noise) * Math.sin(i * 1.7 + seed) * Math.cos(i * 0.6 + seed);
    v = Math.max(10, Math.min(2500, v));
    pts.push(`${x},${(190 - (v * 120) / 2500).toFixed(1)}`);
  }
  return pts.join(" ");
}
const lines = { p50: series(80, 520, 14, 1), p95: series(190, 2150, 30, 2), p99: series(330, 2380, 45, 3) };

// Sleep: the visits of the day, as small ticks under the chart.
const ticks = Array.from({ length: 23 }, (_, i) => {
  const x = 38 + i * 5;
  const h = 4 + Math.round(10 * Math.abs(Math.sin(i * 1.9) * Math.cos(i * 0.7)));
  return { x, h };
});

// Cost: a CPU trace that repeats every 200 units, drawn three times so it can roll.
const period = "L18 92 L22 34 L30 36 L34 92 L96 92 L99 58 L104 62 L108 92 L150 92 L152 74 L156 92 L200 92";

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

// A statement and its film. `row` puts the film beside the words, for a
// block that spans more columns than it is tall.
function Block({
  icon,
  title,
  children,
  stage,
  stageClass,
  className,
  row = false,
}: {
  icon: React.ReactElement;
  title: string;
  children: React.ReactNode;
  stage: React.ReactNode;
  stageClass?: string;
  className?: string;
  row?: boolean;
}) {
  return (
    <article className={cn(glass, "flex flex-col gap-4 p-2.5", row && "md:flex-row md:items-stretch", className)}>
      <div className={cn("grid gap-2 px-3.5 pt-3.5", row && "md:max-w-[34%] md:pb-3.5 md:self-start")}>
        <Heading icon={icon} title={title} />
        <p className="text-sm text-muted-foreground">{children}</p>
      </div>
      <div
        aria-hidden="true"
        className={cn(
          "stage mt-auto flex-1 rounded-[10px] border border-white/80 bg-linear-to-b from-background to-muted/60 dark:border-white/10 dark:from-card dark:to-background",
          row && "md:mt-0",
          stageClass,
        )}
      >
        {stage}
      </div>
    </article>
  );
}

// A picture of the product: the screenshot set in from the top left, running
// off the bottom right of its box, like a device seen at an angle of the page.
function Shot({
  icon,
  title,
  children,
  src,
  alt,
  className,
  close = false,
  clip = false,
}: {
  icon: React.ReactElement;
  title: string;
  children: React.ReactNode;
  src: string;
  alt: string;
  className?: string;
  // A close-up: the picture is one part of a screen, shown whole and near its
  // own size, rather than a screen running off the edge of the box.
  close?: boolean;
  // With `close`: the picture takes the height the row gives it and is cut at
  // the foot, rather than making the row taller.
  clip?: boolean;
}) {
  return (
    <figure className={cn(glass, "flex flex-col gap-4 overflow-hidden p-2.5 pb-0", className)}>
      <figcaption className="grid gap-2 px-3.5 pt-3.5">
        <Heading icon={icon} title={title} />
        <p className="text-sm text-muted-foreground">{children}</p>
      </figcaption>
      {close && clip ? (
        // The screenshots are of the console in its light theme; on the dark
        // page they are turned to dark (the orange kept), as the shipyard is.
        // Cut in a window of its own, as far from the card's foot as from its
        // sides (18px: the card's 10px and 8px of its own), not at the card's
        // edge.
        <div className="relative mx-2 mb-[18px] min-h-60 flex-1 overflow-hidden rounded-[10px] border border-white/80 shadow-[0_6px_16px_rgb(25_25_40/0.07)] dark:border-white/10">
          {/* eslint-disable-next-line @next/next/no-img-element -- the site is a static export */}
          <img
            src={src}
            alt={alt}
            loading="lazy"
            className="absolute inset-x-0 top-0 w-full dark:invert dark:hue-rotate-180"
          />
        </div>
      ) : close ? (
        // As far from the card's foot as from its sides: 18px each (the
        // card's 10px and 8px of its own).
        <div className="flex flex-1 items-center justify-center px-2 pb-[18px]">
          {/* eslint-disable-next-line @next/next/no-img-element -- the site is a static export */}
          <img
            src={src}
            alt={alt}
            loading="lazy"
            className="w-full max-w-[608px] rounded-[10px] border border-white/80 shadow-[0_6px_16px_rgb(25_25_40/0.07)] dark:border-white/10 dark:invert dark:hue-rotate-180"
          />
        </div>
      ) : (
      <div className="relative min-h-60 flex-1 overflow-hidden">
        {/* eslint-disable-next-line @next/next/no-img-element -- the site is a static export */}
        <img
          src={src}
          alt={alt}
          loading="lazy"
          className="absolute top-0 left-6 w-[140%] max-w-none rounded-tl-[10px] border-t border-l border-white/80 shadow-[0_6px_16px_rgb(25_25_40/0.07)] dark:border-white/10 dark:invert dark:hue-rotate-180"
        />
      </div>
      )}
    </figure>
  );
}

export function FeatureBlocks() {
  return (
    <div className="fb mt-8 grid grid-flow-dense gap-4 md:grid-cols-6">
      <Block
        icon={<Bot />}
        title="Your agent runs it"
        row
        className="md:col-span-4"
        stageClass="s-agent"
        stage={
          <>
            <div className="ln ask l1"><span className="pr">›</span><span>why is checkout slow since this morning?</span></div>
            <div className="ln call l2"><i className="dot" /><span className="t">shpyrd releases</span><span className="ok">✓</span></div>
            <div className="ln call l3"><i className="dot" /><span className="t">get_metrics shop · 24h</span><span className="ok">✓</span></div>
            <div className="ln call l4"><i className="dot" /><span className="t">shpyrd logs -p web -n 200</span><span className="ok">✓</span></div>
            <div className="ln ans l5">p95 went from 180 ms to 2.4 s after v42. Roll back to v41?</div>
            <div className="ln act l6"><span className="run">shpyrd rollback</span></div>
          </>
        }
      >
        Claude Code, Codex or Cursor deploy your app, read its logs and find what broke, with the same commands you would type.
      </Block>

      <Block
        icon={<LockKeyhole />}
        title="Security Door"
        className="md:col-span-2 md:row-span-2"
        stageClass="s-door"
        stage={
          <>
            <div className="head"><span>visitors</span><span>expenses</span></div>
            <div className="door-tag">
              <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.3"><rect x="2" y="5.5" width="8" height="5.5" rx="1" /><path d="M4 5.5V4a2 2 0 0 1 4 0v1.5" /></svg>
              Security Door
            </div>
            <div className="door" />
            <div className="lane a"><span className="chip">john@acme.com</span><span className="why">X-Shpyrd-User: john@acme.com</span></div>
            <div className="lane b"><span className="chip">anonymous</span><span className="why">→ sign-in page</span></div>
            <div className="lane c"><span className="chip">emma@gmail.com</span><span className="why">Available to the finance team</span></div>
          </>
        }
      >
        One click puts a sign-in in front of your app. Only the people and teams you choose get in, and the app needs no sign-in code.
      </Block>

      <Block
        icon={<Database />}
        title="Database, cache and disks"
        className="md:col-span-2"
        stageClass="s-res"
        stage={
          <svg viewBox="0 0 400 236" preserveAspectRatio="xMidYMid meet">
            <rect className="frame" x="10" y="10" width="380" height="216" rx="10" />
            <text className="cap" x="22" y="28">project · shop</text>
            <rect className="node app" x="130" y="42" width="140" height="34" rx="8" />
            <text className="lbl" x="200" y="63" textAnchor="middle">shop · web</text>
            <path className="wire w1" pathLength={1} d="M76 176 C76 126, 150 126, 150 76" />
            <path className="wire w2" pathLength={1} d="M200 176 L200 76" />
            <path className="wire w3" pathLength={1} d="M324 176 C324 126, 250 126, 250 76" />
            <rect className="node" x="22" y="176" width="108" height="36" rx="8" />
            <text className="lbl" x="76" y="198" textAnchor="middle">postgres · db</text>
            <rect className="node" x="146" y="176" width="108" height="36" rx="8" />
            <text className="lbl" x="200" y="198" textAnchor="middle">valkey · cache</text>
            <rect className="node" x="270" y="176" width="108" height="36" rx="8" />
            <text className="lbl" x="324" y="198" textAnchor="middle">volume · data</text>
            <g className="var v1"><rect x="66" y="114" width="90" height="20" rx="5" /><text x="111" y="128" textAnchor="middle">DATABASE_URL</text></g>
            <g className="var v2"><rect x="166" y="114" width="68" height="20" rx="5" /><text x="200" y="128" textAnchor="middle">REDIS_URL</text></g>
            <g className="var v3"><rect x="264" y="114" width="46" height="20" rx="5" /><text x="287" y="128" textAnchor="middle">/data</text></g>
          </svg>
        }
      >
        Postgres, Valkey and persistent volumes live in the same project as your app, and arrive as{" "}
        <code className="font-mono text-[0.85em] text-foreground">DATABASE_URL</code> and{" "}
        <code className="font-mono text-[0.85em] text-foreground">REDIS_URL</code>.
      </Block>

      <Shot
        icon={<GitBranch />}
        title="Deploy from Git, or from your folder"
        src="/screenshots/deploy-dialog-crop.png"
        close
        clip
        alt="The Deploy from Git dialog: a repository, a branch and a directory, built with buildpacks"
        className="md:col-span-2"
      >
        Buildpacks or your Dockerfile; new commits on the branch rebuild by themselves.
      </Shot>

      <Block
        icon={<Moon />}
        title="Sleep"
        className="md:col-span-2"
        stageClass="s-sleep"
        stage={
          <svg viewBox="0 0 400 236" preserveAspectRatio="xMidYMid meet">
            <text className="status st-awake" x="20" y="26"><tspan className="k">●</tspan> awake · 1 instance</text>
            <text className="status st-asleep" x="20" y="26"><tspan className="k">◐</tspan> asleep · no CPU, no memory</text>
            <text className="status st-resume" x="20" y="26"><tspan className="k">◌</tspan> resuming… about 6 s</text>
            <line className="gl" x1="34" y1="90" x2="386" y2="90" />
            <line className="gl" x1="34" y1="170" x2="386" y2="170" />
            <text className="ax" x="26" y="93" textAnchor="end">1</text>
            <text className="ax" x="26" y="173" textAnchor="end">0</text>
            <path className="step" d="M34 90 H154 V170 H352 V90 H386" />
            <text className="note" x="160" y="122">sleeps after 10 quiet min</text>
            <g className="anon">
              <text className="note no" x="256" y="152" textAnchor="middle">anonymous · turned away</text>
              <line className="cross" x1="252" y1="186" x2="260" y2="194" />
              <line className="cross" x1="260" y1="186" x2="252" y2="194" />
            </g>
            <text className="note" x="346" y="62" textAnchor="end">ana@acme.com signs in · wakes it</text>
            <g>
              {ticks.map((t) => (
                <rect key={t.x} className="tick" x={t.x} y={198 - t.h} width="2" height={t.h} rx="1" />
              ))}
            </g>
            <rect className="tick wake" x="342" y="184" width="3" height="14" rx="1" />
            <text className="ax" x="34" y="222">09:00</text>
            <text className="ax" x="151" y="222" textAnchor="middle">18:00</text>
            <text className="ax" x="230" y="222" textAnchor="middle">00:00</text>
            <text className="ax" x="308" y="222" textAnchor="middle">06:00</text>
            <text className="ax" x="386" y="222" textAnchor="end">12:00</text>
            <g className="ph"><line x1="34" y1="38" x2="34" y2="204" /></g>
          </svg>
        }
      >
        When nobody uses your app it sleeps, with no CPU or memory to pay. A signed-in visit wakes it in seconds; bots and strangers never do.
      </Block>

      <Block
        icon={<Gauge />}
        title="Small sizes, billed by use"
        row
        className="md:col-span-4"
        stageClass="s-cost"
        stage={
          <>
            <div className="top"><span>agent · worker</span><span className="size">shared-s · 0.5 CPU · 64 MiB</span></div>
            <div className="trace">
              <svg viewBox="0 0 400 100" preserveAspectRatio="none">
                <g className="roll">
                  {[0, 1, 2].map((k) => (
                    <g key={k} transform={`translate(${k * 200} 0)`}>
                      <path className="area" d={`M0 100 L0 92 ${period} L200 100 Z`} />
                      <path className="cpu" d={`M0 92 ${period}`} />
                    </g>
                  ))}
                </g>
              </svg>
              <span className="wait">CPU · mostly waiting for the model</span>
            </div>
            <div className="ladder">
              <span className="first">shared-s</span><span className="bar"><i style={{ width: "6.25%" }} /></span><span className="first">64 MiB</span>
              <span>shared-m</span><span className="bar"><i style={{ width: "25%" }} /></span><span>256 MiB</span>
              <span>shared-l</span><span className="bar"><i style={{ width: "50%" }} /></span><span>512 MiB</span>
              <span>shared-xl</span><span className="bar"><i style={{ width: "100%" }} /></span><span>1 GiB</span>
            </div>
          </>
        }
      >
        Instances start at 64 MiB and CPU is billed by what you use, so an agent waiting for a model costs almost nothing.
      </Block>

      <Block
        icon={<Activity />}
        title="Metrics and logs"
        row
        className="md:col-span-4"
        stageClass="s-met"
        stage={
          <svg viewBox="0 0 400 236" preserveAspectRatio="xMidYMid meet">
            <text className="title" x="20" y="26">Response time</text>
            <g className="lg">
              <line x1="20" y1="42" x2="32" y2="42" className="p50" strokeWidth="2" /><text className="lg" x="36" y="45">p50</text>
              <line x1="66" y1="42" x2="78" y2="42" className="p95" strokeWidth="2" /><text className="lg" x="82" y="45">p95</text>
              <line x1="112" y1="42" x2="124" y2="42" className="p99" strokeWidth="2" /><text className="lg" x="128" y="45">p99</text>
            </g>
            <g className="rng">
              <g><rect x="262" y="12" width="28" height="20" rx="5" /><text x="276" y="26" textAnchor="middle">1h</text></g>
              <g><rect x="294" y="12" width="28" height="20" rx="5" /><text x="308" y="26" textAnchor="middle">6h</text></g>
              <g className="on"><rect x="326" y="12" width="30" height="20" rx="5" /><text x="341" y="26" textAnchor="middle">24h</text></g>
              <g><rect x="360" y="12" width="28" height="20" rx="5" /><text x="374" y="26" textAnchor="middle">7d</text></g>
            </g>
            <line className="gl" x1="34" y1="70" x2="386" y2="70" />
            <line className="gl" x1="34" y1="190" x2="386" y2="190" />
            <text className="ax" x="30" y="73" textAnchor="end">2.5s</text>
            <text className="ax" x="30" y="193" textAnchor="end">0</text>
            <g className="m1"><line className="rel" x1="170" y1="62" x2="170" y2="190" /><text className="rel-t" x="174" y="66">v42</text></g>
            <g className="m2"><line className="rel" x1="290" y1="62" x2="290" y2="190" /><text className="rel-t" x="294" y="66">rollback</text></g>
            <polyline className="ln p50" pathLength={1} points={lines.p50} />
            <polyline className="ln p95" pathLength={1} points={lines.p95} />
            <polyline className="ln p99" pathLength={1} points={lines.p99} />
            <text className="ax" x="34" y="212">12:00</text>
            <text className="ax" x="122" y="212" textAnchor="middle">18:00</text>
            <text className="ax" x="210" y="212" textAnchor="middle">00:00</text>
            <text className="ax" x="298" y="212" textAnchor="middle">06:00</text>
            <text className="ax" x="386" y="212" textAnchor="end">12:00</text>
          </svg>
        }
      >
        Requests, response time, errors, CPU and memory for your web process and every worker, with each release marked on the chart.
      </Block>

      <Shot
        icon={<LayoutDashboard />}
        title="One page for each app"
        src="/screenshots/project-overview-crop.png"
        close
        clip
        alt="A project in shpyrd: its release, processes, resources, members and the actions taken on it"
        className="md:col-span-2"
      >
        Its release, its processes, the databases it uses, who can change it and what they did.
      </Shot>
    </div>
  );
}
