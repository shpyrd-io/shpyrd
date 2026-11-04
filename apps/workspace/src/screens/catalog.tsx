"use client";

import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Database, ExternalLink, GitBranch, HardDrive, KeyRound, LayoutGrid, Search } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { symbolInk, tileOf } from "@shpyrd/ui/components/app-icons";
import { Badge } from "@shpyrd/ui/components/badge";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Tile } from "@shpyrd/ui/components/launcher-card";
import { inks } from "@shpyrd/ui/lib/chart";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { api, ApiError } from "@/api/api";
import type { CatalogEntry, Exposure } from "@/api/types";
import { useInternalExposure } from "@/lib/networking";
import { toneOf } from "@/lib/project";
import { PersonMenu, ThemeButton } from "@/shell/person";

// The catalog: applications ready to run in the workspace. A list to
// pick from, by category or by a word; then the entry, what it asks,
// and the install, which makes a project of it.

// The words a category is listed under.
const categoryNames: Record<string, string> = {
  agents: "Agents",
  crm: "CRM",
  automation: "Automation",
  "developer-tools": "Developer tools",
  analytics: "Analytics",
  communication: "Communication",
  content: "Content",
};
export function categoryName(c: string): string {
  return categoryNames[c] ?? c.replace(/-/g, " ").replace(/^\w/, (m) => m.toUpperCase());
}

// How an entry is drawn: its symbol in its colour, or a window in the
// ink its name keeps.
function lookOf(e: CatalogEntry) {
  const { icon, picture } = tileOf({ icon: e.icon, colour: e.iconColor, file: e.iconUrl ? { src: e.iconUrl, type: "" } : undefined });
  return { icon: icon ?? (picture ? undefined : <LayoutGrid />), picture, colour: e.iconColor, tone: toneOf(e.name) };
}

function EntryTile({ entry, className }: { entry: CatalogEntry; className?: string }) {
  const { icon, picture, colour, tone } = lookOf(entry);
  return (
    <span className={inks[tone]} style={symbolInk(colour)}>
      <Tile icon={icon} picture={picture} className={className} />
    </span>
  );
}

// What an entry asks for, as tags: a key to type, a disk, a database.
function Needs({ entry }: { entry: CatalogEntry }) {
  const keys = entry.inputs.filter((i) => i.required).length;
  return (
    <div className="flex flex-wrap gap-1.5">
      <Badge variant="secondary">{categoryName(entry.category)}</Badge>
      {keys > 0 && (
        <Badge variant="outline">
          <KeyRound /> {keys === 1 ? "A key" : `${keys} keys`}
        </Badge>
      )}
      {entry.volumes.length > 0 && (
        <Badge variant="outline">
          <HardDrive /> Disk
        </Badge>
      )}
      {entry.resources.map((r) => (
        <Badge key={r.kind + r.name} variant="outline">
          <Database /> {r.kind}
        </Badge>
      ))}
    </div>
  );
}

// The top of both pages: the way back, the title, the person.
function Top({ back, title }: { back: { to: string; label: string }; title: string }) {
  return (
    <div className="flex items-center gap-3">
      <Button variant="ghost" size="sm" icon={<ArrowLeft />} asChild>
        <Link to={back.to}>{back.label}</Link>
      </Button>
      <h1 className="font-heading text-xl font-medium">{title}</h1>
      <div className="ml-auto flex items-center gap-1">
        <ThemeButton />
        <PersonMenu />
      </div>
    </div>
  );
}

export function Catalog() {
  const catalog = useQuery({ queryKey: ["catalog"], queryFn: api.catalog, staleTime: 60_000 });
  const [category, setCategory] = useState("all");
  const [words, setWords] = useState("");
  const shown = useMemo(() => {
    const all = catalog.data?.entries ?? [];
    const q = words.trim().toLowerCase();
    return all.filter((e) => (category === "all" || e.category === category) && (!q || [e.name, e.displayName, e.description, ...(e.tags ?? [])].some((s) => s.toLowerCase().includes(q))));
  }, [catalog.data, category, words]);

  return (
    <div className="min-h-svh bg-background">
      <div className="mx-auto grid max-w-6xl gap-8 px-6 pt-5 pb-16 @3xl/page-layout:px-8">
        <Top back={{ to: "/", label: "Launcher" }} title="Catalog" />
        <p className="max-w-2xl text-sm text-muted-foreground">
          Applications ready to run in this workspace. Pick one, give it what it asks, and it becomes a project of yours: built from its repository, following it from then on.
        </p>
        {catalog.isLoading ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-44 rounded-xl" />
            ))}
          </div>
        ) : catalog.error ? (
          <Alert variant="destructive" className="max-w-md">
            <AlertTitle>Could not load the catalog</AlertTitle>
            <AlertDescription>{(catalog.error as Error).message}</AlertDescription>
          </Alert>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-3">
              <Tabs value={category} onValueChange={setCategory}>
                <TabsList>
                  <TabsTrigger value="all">All</TabsTrigger>
                  {catalog.data?.categories.map((c) => (
                    <TabsTrigger key={c} value={c}>
                      {categoryName(c)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
              <Input value={words} onChange={(e) => setWords(e.target.value)} placeholder="Search" aria-label="Search the catalog" prefix={<Search />} className="w-56 sm:ml-auto" />
            </div>
            {shown.length === 0 ? (
              <Blankslate graphic={<LayoutGrid />} title="Nothing here" description="No entry matches. Try another word or category." />
            ) : (
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {shown.map((e) => (
                  <Card key={e.name} className="relative transition-colors hover:bg-muted/40">
                    <CardContent>
                      <Stack gap="cozy">
                        <div className="flex items-start gap-3">
                          <EntryTile entry={e} />
                          <div className="min-w-0">
                            <Link to={`/catalog/${encodeURIComponent(e.name)}`} className="font-medium after:absolute after:inset-0">
                              {e.displayName}
                            </Link>
                            <p className="line-clamp-2 text-sm text-muted-foreground">{e.description}</p>
                          </div>
                        </div>
                        <Needs entry={e} />
                      </Stack>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export function CatalogEntryPage() {
  const { name = "" } = useParams();
  const navigate = useNavigate();
  const queries = useQueryClient();
  const entry = useQuery({ queryKey: ["catalog", name], queryFn: () => api.catalogEntry(name), staleTime: 60_000 });
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const supportsInternal = useInternalExposure();
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [track, setTrack] = useState<"follow" | "pin">("follow");
  const [revision, setRevision] = useState("");
  const [exposure, setExposure] = useState<Exposure>("external");
  const e = entry.data;
  const appHost = config.data?.workspace?.appHost;
  const under = config.data?.workspace?.address ?? config.data?.domain;
  const prefix = appHost?.prefix || undefined;
  const suffix = appHost ? appHost.suffix : under ? `.${under}` : undefined;
  const asked = e?.inputs.filter((i) => !i.generate) ?? [];
  const valueOf = (n: string) => inputs[n] ?? "";
  const missing = asked.filter((i) => i.required && !valueOf(i.name).trim());
  const finalSlug = slug.trim() || e?.name || "";
  const validSlug = /^[a-z0-9-]{2,}$/.test(finalSlug);
  const install = useMutation({
    mutationFn: () =>
      api.installFromCatalog(name, {
        slug: finalSlug,
        name: displayName.trim() || undefined,
        inputs: Object.fromEntries(asked.map((i) => [i.name, valueOf(i.name).trim()]).filter(([, v]) => v)),
        revision: track === "pin" ? revision.trim() || undefined : undefined,
        exposure: supportsInternal ? exposure : undefined,
      }),
    onSuccess: ({ project, notes }) => {
      queries.invalidateQueries({ queryKey: ["projects"] });
      toast.success(`${e?.displayName ?? name} installed as ${project.slug}`, { description: notes?.length ? notes.join(" ") : "It is being built; it answers at its address once the first build is done." });
      navigate(`/projects/${project.slug}`);
    },
    onError: (err: Error) => toast.error(err instanceof ApiError ? err.message : "Could not install"),
  });

  return (
    <div className="min-h-svh bg-background">
      <div className="mx-auto grid max-w-6xl gap-8 px-6 pt-5 pb-16 @3xl/page-layout:px-8">
        <Top back={{ to: "/catalog", label: "Catalog" }} title={e?.displayName ?? "Catalog"} />
        {entry.isLoading ? (
          <Skeleton className="h-64 rounded-xl" />
        ) : entry.error || !e ? (
          <Alert variant="destructive" className="max-w-md">
            <AlertTitle>Could not load the entry</AlertTitle>
            <AlertDescription>{(entry.error as Error | null)?.message ?? "The catalog has no such entry."}</AlertDescription>
          </Alert>
        ) : (
          <div className="grid gap-8 lg:grid-cols-[1fr_24rem]">
            <Stack gap="normal">
              <div className="flex items-start gap-4">
                <EntryTile entry={e} className="size-16 [&_svg]:size-8" />
                <Stack gap="tight" className="min-w-0">
                  <h2 className="font-heading text-2xl font-medium">{e.displayName}</h2>
                  <p className="text-muted-foreground">{e.description}</p>
                  <Needs entry={e} />
                </Stack>
              </div>
              <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[max-content_1fr]">
                <dt className="text-muted-foreground">Source</dt>
                <dd className="flex min-w-0 items-center gap-1.5">
                  <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate font-mono text-xs">{e.source.git}</span>
                  <span className="text-muted-foreground">at {e.source.revision}</span>
                </dd>
                <dt className="text-muted-foreground">Build</dt>
                <dd>
                  {e.build === "dockerfile" ? "Dockerfile" : "Buildpacks"}
                  {e.overlay?.length ? <span className="text-muted-foreground"> · the catalog adds {e.overlay.join(", ")}</span> : null}
                </dd>
                <dt className="text-muted-foreground">Runs</dt>
                <dd>{e.processes.length ? e.processes.join(", ") : "web"}</dd>
                {e.volumes.length > 0 && (
                  <>
                    <dt className="text-muted-foreground">Disk</dt>
                    <dd>{e.volumes.map((v) => `${v.name}, ${v.size}`).join("; ")}</dd>
                  </>
                )}
                {e.resources.length > 0 && (
                  <>
                    <dt className="text-muted-foreground">Resources</dt>
                    <dd>{e.resources.map((r) => `${r.kind} ${r.name}`).join("; ")}</dd>
                  </>
                )}
                {(e.publisher || e.license) && (
                  <>
                    <dt className="text-muted-foreground">By</dt>
                    <dd>
                      {e.publisher}
                      {e.license ? <span className="text-muted-foreground"> · {e.license}</span> : null}
                    </dd>
                  </>
                )}
                {e.homepage && (
                  <>
                    <dt className="text-muted-foreground">Homepage</dt>
                    <dd>
                      <a href={e.homepage} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 underline underline-offset-4">
                        {e.homepage.replace(/^https?:\/\//, "")} <ExternalLink className="size-3" />
                      </a>
                    </dd>
                  </>
                )}
              </dl>
              {e.readme && (
                <div className="rounded-lg border bg-muted/30 p-4">
                  <pre className="font-sans text-sm whitespace-pre-wrap">{e.readme.trim()}</pre>
                </div>
              )}
            </Stack>

            {/* The install: the project's name and address, what the entry asks, and where it follows upstream. */}
            <Card>
              <CardContent>
                <Stack gap="normal">
                  <h2 className="font-heading text-lg font-medium">Install</h2>
                  <Field label="Slug" hint="Lowercase letters, digits and dashes." error={slug && !validSlug ? "Only lowercase letters, digits and dashes." : undefined}>
                    <Input value={slug} onChange={(ev) => setSlug(ev.target.value.toLowerCase())} placeholder={e.name} prefix={prefix} suffix={suffix} />
                  </Field>
                  <Field label="Name">
                    <Input value={displayName} onChange={(ev) => setDisplayName(ev.target.value)} placeholder={e.displayName} />
                  </Field>
                  {asked.map((i) => (
                    <Field key={i.name} label={i.label} hint={i.help} required={i.required}>
                      <Input
                        type={i.secret ? "password" : "text"}
                        autoComplete="off"
                        value={valueOf(i.name)}
                        onChange={(ev) => setInputs((all) => ({ ...all, [i.name]: ev.target.value }))}
                        placeholder={i.default}
                        className={i.secret ? "font-mono text-xs" : undefined}
                      />
                    </Field>
                  ))}
                  <Field label="Updates" hint={track === "follow" ? `Every new commit of ${e.source.revision} is built and released.` : "Nothing changes until you change the revision."}>
                    <Select value={track} onValueChange={(v) => setTrack(v as "follow" | "pin")}>
                      <SelectTrigger aria-label="Updates" className="w-full"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="follow">Follow {e.source.revision}</SelectItem>
                        <SelectItem value="pin">Pin a tag or commit</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                  {track === "pin" && (
                    <Field label="Tag or commit" required>
                      <Input value={revision} onChange={(ev) => setRevision(ev.target.value)} placeholder="v1.2.0" className="font-mono text-xs" />
                    </Field>
                  )}
                  {supportsInternal && (
                    <Field label="Network" hint="Internal apps are reachable only through this workspace's private network.">
                      <Select value={exposure} onValueChange={(v) => setExposure(v as Exposure)}>
                        <SelectTrigger aria-label="Network" className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="external">Public internet</SelectItem>
                          <SelectItem value="internal">Internal network</SelectItem>
                        </SelectContent>
                      </Select>
                    </Field>
                  )}
                  <Button disabled={!validSlug || missing.length > 0 || (track === "pin" && !revision.trim()) || install.isPending} onClick={() => install.mutate()}>
                    {install.isPending ? "Installing…" : "Install"}
                  </Button>
                  {missing.length > 0 && <p className="text-xs text-muted-foreground">Needs {missing.map((i) => i.label).join(", ")}.</p>}
                </Stack>
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  );
}
