"use client";

import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, LayoutGrid, Plus, Settings } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { LauncherCard } from "@shpyrd/ui/components/launcher-card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Separator } from "@shpyrd/ui/components/separator";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api, ApiError } from "@/api/api";
import type { Exposure, ProjectSummary } from "@/api/types";
import { useInternalExposure } from "@/lib/networking";
import { useBrandColor } from "@/lib/branding";
import { CopyLine } from "@/lib/copy-line";
import { usePerms, useUserOnly } from "@/lib/perms";
import { addressOf, badgesOf, hostOf, openUrl, phaseOf } from "@/lib/project";
import { lookOf } from "@/lib/tile";
import { PersonMenu, ThemeButton } from "@/shell/person";

// Where everyone lands: the applications of the workspace, one card
// each. Who operates sees the gear of each and the way to make one more;
// the big gear is the workspace itself.
export function Launcher() {
  const navigate = useNavigate();
  const perms = usePerms();
  const userOnly = useUserOnly(perms);
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const projects = useQuery({ queryKey: ["projects"], queryFn: api.projects, refetchInterval: 15_000 });
  const workspace = config.data?.workspace;
  useBrandColor(workspace?.branding?.color);

  return (
    <div className="relative min-h-svh bg-background">
      <div className="absolute top-5 right-5 flex items-center gap-1">
        <ThemeButton />
        <PersonMenu />
        {perms.admin && (
          <Button variant="outline" icon={<Settings />} onClick={() => navigate("/workspace")} className="ml-2">
            Settings
          </Button>
        )}
        {perms.create && <NewProject />}
      </div>
      <div className="mx-auto grid max-w-6xl justify-items-center gap-10 px-6 pt-20 pb-16 @3xl/page-layout:px-8">
        <div className="grid justify-items-center gap-3 text-center">
          {workspace?.branding?.logoUrl ? (
            <img src={workspace.branding.logoUrl} alt="" className="h-14 max-w-40 object-contain" />
          ) : (
            <LogoMark className="size-14" />
          )}
          <h1 className="font-heading text-2xl font-medium">{workspace?.name ?? <Skeleton className="h-7 w-32" />}</h1>
        </div>

        {projects.isLoading ? (
          <div className="flex flex-wrap justify-center gap-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-60 w-64 rounded-xl" />
            ))}
          </div>
        ) : projects.error ? (
          <Alert variant="destructive" className="max-w-md">
            <AlertTitle>Could not load the applications</AlertTitle>
            <AlertDescription>{(projects.error as Error).message}</AlertDescription>
          </Alert>
        ) : projects.data && projects.data.length > 0 ? (
          <div className="flex flex-wrap justify-center gap-4">
            {projects.data.map((p) => (
              <Card key={p.slug} project={p} operator={!userOnly} />
            ))}
          </div>
        ) : perms.create ? (
          <Blankslate
            graphic={<LayoutGrid />}
            title="No application yet"
            description="Ask your agent to put your app online here, or make an empty one to deploy to later. It appears here for everyone who may open it."
            action={<NewProject asButton />}
          />
        ) : (
          <Blankslate
            graphic={<LayoutGrid />}
            title="Your applications will appear here"
            description="When a project of this workspace lets you in, it shows up on this page."
          />
        )}
      </div>
      {config.data?.version && (
        <span className="absolute right-5 bottom-4 font-mono text-[11px] text-muted-foreground/60">{config.data.version}</span>
      )}
    </div>
  );
}

// One project: its symbol, where it answers (its own domain first), and
// who may open it.
function Card({ project, operator }: { project: ProjectSummary; operator: boolean }) {
  const navigate = useNavigate();
  const address = addressOf(project);
  return (
    <LauncherCard
      name={project.displayName}
      description={project.description}
      {...lookOf(project)}
      url={hostOf(address)}
      href={openUrl(address, project.access)}
      tags={badgesOf(project)}
      phase={phaseOf(project)}
      exposure={project.exposure === "internal" ? "internal" : "public"}
      access={project.access === "authenticated" ? "locked" : "open"}
      onSettings={operator ? () => navigate(`/projects/${project.slug}`) : undefined}
    />
  );
}

// The dialog that makes a project. It is the "+" at the top, or the
// button of the empty page.
function NewProject({ asButton = false }: { asButton?: boolean }) {
  const queries = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState("");
  // The address follows the name until it is typed itself.
  const [slugEdited, setSlugEdited] = useState(false);
  const supportsInternal = useInternalExposure();
  const [exposure, setExposure] = useState<Exposure>("external");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [git, setGit] = useState("");
  const [ref, setRef] = useState("");
  const [path, setPath] = useState("");
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  // The app's address around the slug: acme-<slug>.shpyrd.app, or
  // <slug>.<domain> where the server gives no pattern.
  const appHost = config.data?.workspace?.appHost;
  const under = config.data?.workspace?.address ?? config.data?.domain;
  const prefix = appHost?.prefix || undefined;
  const suffix = appHost ? appHost.suffix : under ? `.${under}` : undefined;
  const create = useMutation({
    mutationFn: () =>
      api.createProject({
        slug: slug.trim(),
        exposure,
        displayName: name.trim() || undefined,
        description: description.trim() || undefined,
        git: git.trim() ? { url: git.trim(), revision: ref.trim() || "main" } : undefined,
        subPath: path.trim() || undefined,
      }),
    onSuccess: (p) => {
      queries.invalidateQueries({ queryKey: ["projects"] });
      toast.success(`Project ${p.slug} created`, { description: "Deploy something to it and it answers at its address." });
      setOpen(false);
      navigate(`/projects/${p.slug}`);
    },
    onError: (e: Error) => toast.error(e instanceof ApiError ? e.message : "Could not create the project"),
  });
  const validSlug = /^[a-z0-9-]{2,}$/.test(slug.trim());
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {asButton ? (
          <Button icon={<Plus />}>New project</Button>
        ) : (
          <Button icon={<Plus />} className="ml-1">
            New project
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto">
        <DialogHeader divider>
          <DialogTitle>New project</DialogTitle>
          <DialogDescription>The quickest way is to ask the agent you build with. It makes the project, deploys it and tells you the address.</DialogDescription>
        </DialogHeader>
        <Stack gap="normal">
          <CopyLine icon={<Bot />} text={`Put this app online on ${config.data?.workspace?.name ?? "shpyrd"}`} />
          <p className="text-xs text-muted-foreground">
            Agent not set up yet?{" "}
            <a href="https://shpyrd.io/docs/getting-started" target="_blank" rel="noreferrer" className="text-primary underline-offset-4 hover:underline">
              Add shpyrd to it
            </a>{" "}
            in a minute.
          </p>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <Separator className="flex-1" />
            or make it here
            <Separator className="flex-1" />
          </div>
          <Field label="Name" required>
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!slugEdited) setSlug(slugOf(e.target.value));
              }}
              placeholder="Hello World"
            />
          </Field>
          <Field label="Address" hint="Lowercase letters, digits and dashes. It cannot change later." error={slug && !validSlug ? "Only lowercase letters, digits and dashes." : undefined}>
            <Input
              value={slug}
              onChange={(e) => {
                setSlugEdited(true);
                setSlug(e.target.value.toLowerCase());
              }}
              placeholder="hello-world"
              prefix={prefix}
              suffix={suffix}
            />
          </Field>
          <Field label="Git repository" hint="Optional: it is built and put online right away.">
            <Input value={git} onChange={(e) => setGit(e.target.value)} placeholder="https://github.com/acme/hello-world" className="font-mono text-xs" />
          </Field>
          <details className="group text-sm">
            <summary className="cursor-pointer text-muted-foreground select-none hover:text-foreground">More options</summary>
            <Stack gap="normal" className="pt-4">
              <Field label="What it is" hint="One line, on its card.">
                <Textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="A small API that answers the mobile app." />
              </Field>
              {git.trim() && (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Branch, tag or commit">
                    <Input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="main" className="font-mono text-xs" />
                  </Field>
                  <Field label="Directory">
                    <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="services/api" className="font-mono text-xs" />
                  </Field>
                </div>
              )}
              {supportsInternal && (
                <Field label="Network" hint="Internal apps are reachable only through this workspace's private network.">
                  <Select value={exposure} onValueChange={(v) => setExposure(v as Exposure)}>
                    <SelectTrigger aria-label="Network"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="external">Public internet</SelectItem>
                      <SelectItem value="internal">Internal network</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              )}
            </Stack>
          </details>
          {!supportsInternal && exposure === "internal" && <p className="text-sm text-destructive">Internal exposure is no longer available. <button type="button" className="underline" onClick={() => setExposure("external")}>Choose public internet</button> to continue.</p>}
        </Stack>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button disabled={!validSlug || create.isPending || (exposure === "internal" && !supportsInternal)} onClick={() => create.mutate()}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// "Hello World!" → "hello-world".
function slugOf(name: string) {
  return name.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

export { Link };
