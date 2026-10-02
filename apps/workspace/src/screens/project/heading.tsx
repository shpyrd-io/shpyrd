"use client";

import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Globe, Hammer, Lock, Network, Pencil, RotateCcw, Rocket, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertActions, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import type { IconChoice } from "@shpyrd/ui/components/app-icons";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { DropdownButton } from "@shpyrd/ui/components/dropdown-button";
import { DropdownMenuItem } from "@shpyrd/ui/components/dropdown-menu";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { IconPicker } from "@shpyrd/ui/components/icon-picker";
import { Input } from "@shpyrd/ui/components/input";
import { TextLogView } from "@shpyrd/ui/components/log-view";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { api } from "@/api/api";
import { buildFailed, type DeployRequest, type Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { addressOf, hostOf, openUrl, phaseOf, phaseWords } from "@/lib/project";
import { choiceOf, ProjectTile } from "@/lib/tile";
import { useStream } from "./shared";

const types = { running: "success", deploying: "warning", failed: "error", sleeping: "neutral" } as const;

// A release is on its way: building or rolling out. What would start
// another waits.
export const busy = (p: Project) => p.status.phase === "Building" || p.status.phase === "Deploying";

// The name, how it is, where it answers, and what can be done: over
// every page of the project. Under it, what is going on right now.
export function Heading({ project, perms }: { project: Project; perms: Perms }) {
  const navigate = useNavigate();
  const queries = useQueryClient();
  const phase = phaseOf(project);
  const address = addressOf({ url: project.status.url ?? project.url, domain: project.domain });
  const host = hostOf(address);
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["project", project.slug] });
    queries.invalidateQueries({ queryKey: ["projects"] });
  };
  const destroy = useMutation({
    mutationFn: () => api.destroyProject(project.slug),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["projects"] });
      toast.success(`${project.displayName} destroyed`);
      navigate("/");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const redeploy = useMutation({
    mutationFn: (action?: "restart" | "rebuild") => api.redeploy(project.slug, action),
    onSuccess: (r) => {
      toast.success(r.message);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const expose = useMutation({
    mutationFn: (exposure: "external" | "internal") => api.setExposure(project.slug, exposure),
    onSuccess: (_, exposure) => {
      toast.success(exposure === "internal" ? "Now on the local network only" : "Now on the public internet");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const internal = project.exposure === "internal";
  const lastBuildFailed = buildFailed(project.status);
  const processes = Object.entries(project.processes ?? {});
  const release = project.status.release;
  return (
    <>
      <PageHeading
        title={project.displayName}
        icon={<ProjectTile project={project} className="size-9 rounded-md" />}
        iconEnd={
          <>
            {project.displayName !== project.slug && <InlineCode>{project.slug}</InlineCode>}
            {perms.config && <Describe project={project} onDone={refresh} />}
            <StatusBadge type={types[phase]} live={phase === "deploying"}>
              {phaseWords[phase]}
            </StatusBadge>
            {perms.deploy ? (
              <ConfirmDialog
                trigger={
                  <Badge variant="outline" className="cursor-pointer gap-1 hover:bg-muted" title={internal ? "On the local network. Click to put it on the public internet." : "On the public internet. Click to keep it on the local network."}>
                    {internal ? <Network /> : <Globe />}
                    {internal ? "local network" : "public internet"}
                  </Badge>
                }
                title={internal ? "Put it on the public internet?" : "Keep it on the local network?"}
                description={internal ? "It answers to the internet at its address, through the public load balancer." : "Only the cluster's network reaches it, through the private load balancer. Its public address stops answering."}
                action={internal ? "Public internet" : "Local network"}
                onConfirm={() => expose.mutate(internal ? "external" : "internal")}
              />
            ) : (
              <Badge variant="outline" className="gap-1">
                {internal ? <Network /> : <Globe />}
                {internal ? "local network" : "public internet"}
              </Badge>
            )}
            {project.access !== "public" && (
              <Badge variant="outline" className="gap-1">
                <Lock /> {project.access === "identified" ? "named people" : "who signs in"}
              </Badge>
            )}
          </>
        }
        description={host}
        actions={
          <>
            {host && (
              <Button variant="outline" size="sm" iconEnd={<ExternalLink />} asChild>
                <a href={openUrl(address, project.access)} target="_blank" rel="noreferrer">
                  Open
                </a>
              </Button>
            )}
            {perms.deploy && (
              <Deploy project={project} onDone={refresh}>
                <DropdownMenuItem disabled={redeploy.isPending || busy(project)} onClick={() => redeploy.mutate(undefined)}>
                  <RotateCcw /> {lastBuildFailed ? "Build the same source again" : "Redeploy the current release"}
                </DropdownMenuItem>
                <DropdownMenuItem disabled={redeploy.isPending || busy(project)} onClick={() => redeploy.mutate("rebuild")}>
                  <Hammer /> Rebuild from the source
                </DropdownMenuItem>
              </Deploy>
            )}
            {perms.destroy && (
              <ConfirmDialog
                trigger={<Button variant="destructive" size="sm" icon={<Trash2 />} aria-label="Destroy the project" />}
                variant="destructive"
                title={`Destroy ${project.displayName}?`}
                description="Its instances stop, its volumes are removed and its address is released. This cannot be undone."
                confirmation={project.slug}
                action="Destroy the project"
                onConfirm={() => destroy.mutate()}
              />
            )}
          </>
        }
      />
      {project.status.phase === "Pending" && !project.spec.source && (
        <Alert variant="info" icon={<Rocket />}>
          <AlertTitle>Nothing deployed yet</AlertTitle>
          <AlertDescription>
            Deploy from a Git repository with the button above, or run <InlineCode>shpyrd deploy --project {project.slug}</InlineCode> from a checkout.
          </AlertDescription>
        </Alert>
      )}
      {project.status.phase === "Building" && <Building project={project} />}
      {release && release.state === "Running" && project.status.phase === "Deploying" && <ReleaseCommand project={project} perms={perms} onRetry={() => redeploy.mutate("restart")} retrying={redeploy.isPending} />}
      {release && release.state === "Failed" && project.status.phase === "Failed" && <ReleaseCommand project={project} perms={perms} onRetry={() => redeploy.mutate("restart")} retrying={redeploy.isPending} />}
      {phase === "deploying" && project.status.phase === "Deploying" && !(release && release.state === "Running") && (
        <Alert variant="info">
          <AlertTitle>{project.status.message ?? "Rolling out"}</AlertTitle>
          <AlertDescription>
            The new release takes the instances one at a time; the old ones answer until then.
            {processes.some(([, p]) => p.updated !== undefined) && (
              <span className="mt-1 block font-mono text-xs">{processes.map(([name, p]) => `${name} ${Math.min(p.updated ?? 0, p.desired)}/${p.desired} on the new release, ${p.ready} serving${p.failing ? `, ${p.failing} failing` : ""}`).join(" · ")}</span>
            )}
          </AlertDescription>
        </Alert>
      )}
      {phase === "failed" && !(release && release.state === "Failed") && (
        <Alert variant="destructive">
          <AlertTitle>{lastBuildFailed ? "The build failed" : "The release is not healthy"}</AlertTitle>
          <AlertDescription>{project.status.message ?? project.message ?? "An instance keeps failing."}</AlertDescription>
        </Alert>
      )}
    </>
  );
}

// The build going on, as it prints.
function Building({ project }: { project: Project }) {
  const build = project.status.latestBuild;
  const lines = useStream<string>(build ? (signal, push) => api.streamBuild(project.slug, build, true, signal, push) : null, [project.slug, build]);
  return (
    <Alert variant="info" icon={<Hammer className="animate-pulse" />}>
      <AlertTitle>
        Building {build && <InlineCode>{build}</InlineCode>}
      </AlertTitle>
      <AlertDescription>{project.status.message}</AlertDescription>
      <TextLogView lines={lines.lines} height={256} empty={build ? "Waiting for the build to start…" : "Waiting for a builder…"} className="mt-3" />
    </Alert>
  );
}

// The release command of the release going out, or the one that failed.
function ReleaseCommand({ project, perms, onRetry, retrying }: { project: Project; perms: Perms; onRetry: () => void; retrying: boolean }) {
  const release = project.status.release!;
  const running = release.state === "Running";
  const command = release.message?.replace(/^running /, "") ?? "the release command";
  const output = useStream<string>(
    (signal, push) => api.streamLogs(project.slug, { process: "release", tail: 60, follow: running }, signal, (l) => push(l.message)),
    [project.slug, running],
  );
  return (
    <Alert variant={running ? "warning" : "destructive"}>
      <AlertTitle>
        {running ? "Release command running" : "The release command failed"} {running && <InlineCode>{command}</InlineCode>}
      </AlertTitle>
      <AlertDescription>{running ? "It runs before the new release rolls out; the current one keeps answering meanwhile." : release.message}</AlertDescription>
      <TextLogView lines={output.lines} height={200} empty={running ? "Waiting for output…" : "No output was kept."} className="mt-3" />
      {!running && perms.deploy && (
        <AlertActions>
          <Button size="sm" variant="outline" icon={<RotateCcw />} disabled={retrying} onClick={onRetry}>
            Run it again
          </Button>
          <span className="text-xs text-muted-foreground">or fix the command and deploy again</span>
        </AlertActions>
      )}
    </Alert>
  );
}

// Deploy from Git: the repository, the revision, the directory and how
// it is built. A local checkout goes through the CLI.
function Deploy({ project, onDone, children }: { project: Project; onDone: () => void; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const [git, setGit] = useState(project.spec.source?.git?.url ?? "");
  const [ref, setRef] = useState(project.spec.source?.git?.revision ?? "");
  const [path, setPath] = useState(project.spec.source?.subPath ?? "");
  const [strategy, setStrategy] = useState<NonNullable<DeployRequest["strategy"]>>(project.spec.build?.strategy ?? "buildpacks");
  const [dockerfile, setDockerfile] = useState(project.spec.build?.dockerfile ?? "");
  const deploy = useMutation({
    mutationFn: () => api.deploy(project.slug, { git: { url: git.trim(), revision: ref.trim() || "main" }, subPath: path.trim() || undefined, strategy, dockerfile: strategy === "dockerfile" ? dockerfile.trim() || undefined : undefined }),
    onSuccess: () => {
      toast.success("Deploy requested", { description: "Building; the release follows." });
      setOpen(false);
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const waiting = busy(project);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DropdownButton size="sm" label="Deploy" disabled={waiting} title={waiting ? "Wait for the current release to finish" : undefined} onClick={() => setOpen(true)}>
        {children}
      </DropdownButton>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Deploy from Git</DialogTitle>
          <DialogDescription>
            The repository is built with buildpacks or its Dockerfile, and released. To deploy a local checkout use <InlineCode>shpyrd deploy</InlineCode>.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (git.trim()) deploy.mutate();
          }}
        >
          <Field label="Repository">
            <Input value={git} onChange={(e) => setGit(e.target.value)} placeholder="https://github.com/acme/hello-world" autoFocus className="font-mono text-xs" />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Branch, tag or commit">
              <Input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="main" className="font-mono text-xs" />
            </Field>
            <Field label="Directory" hint="When the application is not at the root.">
              <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="services/api" className="font-mono text-xs" />
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Build with">
              <Select value={strategy} onValueChange={(v) => setStrategy(v as typeof strategy)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="buildpacks">Buildpacks, the stack detected</SelectItem>
                  <SelectItem value="dockerfile">Its Dockerfile</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            {strategy === "dockerfile" && (
              <Field label="Dockerfile">
                <Input value={dockerfile} onChange={(e) => setDockerfile(e.target.value)} placeholder="Dockerfile" className="font-mono text-xs" />
              </Field>
            )}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" icon={<Rocket />} disabled={!git.trim() || deploy.isPending}>
              Deploy
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// What people see on the launcher: the name, the line under it, and the
// icon of the card. The slug never changes.
function Describe({ project, onDone }: { project: Project; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(project.displayName);
  const [description, setDescription] = useState(project.description ?? "");
  const [choice, setChoice] = useState<IconChoice>(choiceOf(project));
  useEffect(() => {
    if (open) {
      setName(project.displayName);
      setDescription(project.description ?? "");
      setChoice(choiceOf(project));
    }
    // Only when it opens: what is being typed is not reset by a refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const iconChanged = (choice.icon ?? "") !== (project.icon ?? "") || (choice.colour ?? "") !== (project.iconColor ?? "");
  const fileChanged = choice.file?.src !== project.iconUrl;
  const save = useMutation({
    mutationFn: async () => {
      await api.updateProject(project.slug, {
        name: name.trim(),
        description: description.trim(),
        ...(iconChanged ? { icon: choice.icon ?? "", iconColor: choice.colour ?? "" } : {}),
      });
      // The image of its own: sent when it is a new one, removed when a
      // symbol took its place.
      if (fileChanged) {
        if (choice.file) await api.setProjectIcon(project.slug, choice.file.src);
        else await api.removeProjectIcon(project.slug);
      }
    },
    onSuccess: () => {
      toast.success("Saved");
      setOpen(false);
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const dirty = name.trim() !== project.displayName || description.trim() !== (project.description ?? "") || iconChanged || fileChanged;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon-xs" icon={<Pencil />} aria-label="Name, icon and description" className="text-muted-foreground" />
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader divider>
          <DialogTitle>Name, icon and description</DialogTitle>
          <DialogDescription>
            What people see on the launcher. The slug <InlineCode>{project.slug}</InlineCode> stays: it is the address and what the CLI uses.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim() && dirty) save.mutate();
          }}
        >
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <Field label="Description" hint="One line under the name on the launcher.">
            <Input value={description} maxLength={200} onChange={(e) => setDescription(e.target.value)} placeholder="Operations of the day, finance and the team." />
          </Field>
          <IconPicker value={choice} onChange={setChoice} />
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!name.trim() || !dirty || save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
