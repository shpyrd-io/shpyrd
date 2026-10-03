"use client";

import { useId, useRef, useState } from "react";
import { useIsMutating, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import type { ProjectArchiveActions } from "./api/project-archives";

const phaseLabels: Record<string, string> = {
  measuring: "Measuring data; project stays online", copying: "Copying data to the destination node", preparing: "Preparing maintenance", paused: "Project paused", validating: "Validating archive", staging: "Preparing restored data",
  committing: "Replacing project data", verifying: "Verifying restoration", starting: "Starting project", releasing: "Resuming requests",
  complete: "Finishing cleanup", "rolling-back": "Recovering previous data", "rolled-back": "Previous data recovered", "recovery-required": "Recovery required",
};
type Operation = "backup" | "restore" | "recover";

export function ProjectBackups({ name, queryKey, actions, allowed = true }: {
  name: string; queryKey: string; actions: ProjectArchiveActions; allowed?: boolean;
}) {
  const id = useId();
  const [file, setFile] = useState<File | null>(null);
  const [message, setMessage] = useState("");
  const submitting = useRef(false);
  const queries = useQueryClient();
  const mutationKey = ["project-archive-operation", queryKey];
  const pending = useIsMutating({ mutationKey }) > 0;
  const status = useQuery({ queryKey: ["project-archive", queryKey], queryFn: actions.status, enabled: allowed, refetchInterval: 2_000 });
  const preview = status.data?.preview === true;
  const operation = useMutation({
    mutationKey,
    mutationFn: async (kind: Operation) => {
      setMessage("");
      if (kind === "restore") {
        if (!file) throw new Error("Choose a project archive first.");
        await actions.restore(file);
      } else await actions[kind]();
      return kind;
    },
    onSuccess: (kind) => {
      setMessage(preview
        ? `Preview finished. ${kind === "backup" ? "No backup file was generated or downloaded." : "No project data was changed."}`
        : kind === "backup" ? "Backup prepared; the download was requested. Check your browser's downloads. Keep the archive somewhere safe; it contains project data and secrets."
        : kind === "restore" ? "The project has been restored." : "Recovery finished.");
    },
    onSettled: () => {
      submitting.current = false;
      void queries.invalidateQueries();
    },
  });
  const phase = status.data?.phase ?? "idle";
  const running = pending || operation.isPending || status.data?.active === true;
  const busy = running || phase !== "idle";
  const disabled = busy || status.isLoading || !!status.error;
  const kind = operation.isPending ? operation.variables
    : status.data?.active ? status.data.kind === "export" ? "backup" : status.data.kind === "move" ? "move" : status.data.kind === "restore" ? "restore" : undefined : undefined;
  const progress = phase !== "idle" ? phaseLabels[phase] ?? "Project maintenance"
    : kind === "backup" ? "Preparing backup"
    : kind === "restore" ? preview ? "Preparing restoration" : "Uploading and validating archive"
    : kind === "move" ? "Moving project resources" : kind === "recover" ? "Recovering project" : "Preparing operation";
  function start(next: Operation) {
    if (submitting.current || running || status.isLoading || status.error || (next !== "recover" && busy)) return;
    submitting.current = true;
    operation.mutate(next);
  }
  const spinner = <Loader2 aria-hidden="true" className="animate-spin" />;
  const failure = operation.error ?? status.error;
  if (!allowed) return <p className="text-sm text-muted-foreground">Project backups are available to project and workspace administrators.</p>;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Project backup and restore</CardTitle>
        <CardDescription>Download one .tgz with the code, deployed build, configuration, PostgreSQL databases and volume files. The project pauses during backup and restore.</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-5">
        {preview && <Alert><AlertTitle>Interactive preview</AlertTitle><AlertDescription>These actions simulate progress. No project is paused, no backup file is downloaded, and restore does not read or upload your selected file.</AlertDescription></Alert>}
        <p className="text-sm text-muted-foreground">While paused, requests receive HTTP 503 with Retry-After. External services must support retries. There is no saved backup history: temporary downloads expire after five minutes.</p>
        {(running || phase !== "idle") && <Alert role="status" aria-live="polite" icon={running ? spinner : undefined}>
          <AlertTitle>{preview ? "Preview: " : ""}{progress}{running ? "…" : ""}</AlertTitle>
          <AlertDescription>{status.data?.resource || (running ? "Keep this page open. Backup and restore controls are disabled until the operation finishes." : "The operation is no longer running. Recover it before starting another backup or restore.")}{status.data?.error && <p>{status.data.error}</p>}</AlertDescription>
        </Alert>}
        {failure && <Alert variant="destructive"><AlertTitle>The operation could not finish</AlertTitle><AlertDescription>{(failure as Error).message}</AlertDescription></Alert>}
        {message && <p role="status" className="text-sm">{message}</p>}
        <ConfirmDialog
          title={preview ? `Preview backup of ${name}?` : `Back up ${name}?`}
          description={preview ? "Simulate backup progress. No project will be paused and no file will be downloaded." : "The project will stop briefly so its database and files are captured together. It will resume before the download starts."}
          action={preview ? "Simulate backup" : "Pause and back up"}
          onConfirm={() => start("backup")}
          trigger={<Button variant="outline" disabled={disabled} aria-busy={running && kind === "backup"} icon={running && kind === "backup" ? spinner : undefined}>{running && kind === "backup" ? "Preparing backup…" : "Download project backup"}</Button>}
        />
        <div className="grid gap-2">
          <Label htmlFor={id}>Restore from a project archive</Label>
          <Input id={id} type="file" accept=".tgz,.gz,application/gzip" disabled={disabled} onChange={(event) => { setFile(event.target.files?.[0] ?? null); setMessage(""); }} />
          <p className="text-xs text-muted-foreground">Restore replaces this project's code, configuration and data. Its address, access rules and workspace stay the same. You can also restore into a newly created empty project.</p>
        </div>
        <ConfirmDialog
          title={preview ? `Preview restore of ${name}?` : `Restore ${name}?`}
          description={preview ? "Simulate restoration progress. The selected file will not be read or uploaded, and no project data will change." : `Replace this project's contents with ${file?.name ?? "the selected archive"}. Changes made after that backup will be lost. Save a current backup first if you need to keep them.`}
          confirmation={name} action={preview ? "Simulate restore" : "Pause and restore"} variant="destructive"
          onConfirm={() => start("restore")}
          trigger={<Button variant="outline" disabled={disabled || !file} aria-busy={running && kind === "restore"} icon={running && kind === "restore" ? spinner : undefined}>{running && kind === "restore" ? "Restoring project…" : "Restore project"}</Button>}
        />
        {((phase !== "idle" && !status.data?.active && !pending) || (operation.isPending && kind === "recover")) && <ConfirmDialog
          title="Recover interrupted maintenance?"
          description="If restoration was interrupted before requests resumed, recover the previous project data. If requests already resumed, only finish cleanup; new writes are preserved."
          action="Recover project" onConfirm={() => start("recover")}
          trigger={<Button variant="outline" disabled={running || !!status.error} aria-busy={running} icon={running ? spinner : undefined}>{running ? "Recovering project…" : "Recover interrupted operation"}</Button>}
        />}
      </CardContent>
    </Card>
  );
}
