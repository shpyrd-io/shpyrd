"use client";

import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Toaster } from "@shpyrd/ui/components/sonner";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

// A word in the corner, for a moment: what was just done, or what went
// wrong. It is Sonner, with our icons and colours. An application mounts
// one `Toaster` and calls `toast` from anywhere.
export default function Page() {
  return (
    <>
      <Toaster position="bottom-right" />
      <Section title="Kinds">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button variant="outline" onClick={() => toast("Deploying v13")}>
            Plain
          </Button>
          <Button variant="outline" onClick={() => toast.success("Project hello-world created")}>
            Success
          </Button>
          <Button variant="outline" onClick={() => toast.info("A new version of the CLI is out")}>
            Info
          </Button>
          <Button variant="outline" onClick={() => toast.warning("The disk is almost full")}>
            Warning
          </Button>
          <Button variant="outline" onClick={() => toast.error("The worker could not start")}>
            Error
          </Button>
        </Stack>
      </Section>
      <Section title="With what explains it">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button
            variant="outline"
            onClick={() =>
              toast.success("Workspace acme created", {
                description: "It answers at acme.shpyrd.app. Ana was invited as its first owner.",
              })
            }
          >
            Success, with a description
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              toast.error("Rollback to v10 failed", {
                description: "The build of v10 has no worker process; those instances would not start.",
              })
            }
          >
            Error, with a description
          </Button>
        </Stack>
      </Section>
      <Section title="With something to do">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button
            variant="outline"
            onClick={() =>
              toast("Project hello removed", {
                action: { label: "Undo", onClick: () => toast.success("hello is back") },
              })
            }
          >
            With an action
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              toast.info("Build #49 is running", {
                action: { label: "Follow", onClick: () => {} },
                duration: 8000,
              })
            }
          >
            That stays longer
          </Button>
        </Stack>
      </Section>
      <Section title="While it waits, then what came of it">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button
            variant="outline"
            onClick={() =>
              toast.promise(new Promise((done) => setTimeout(done, 1800)), {
                loading: "Deploying v13…",
                success: "v13 is out, on 4 instances",
                error: "The deploy failed",
              })
            }
          >
            Deploy
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              toast.promise(new Promise((_, fail) => setTimeout(fail, 1800)), {
                loading: "Restarting the worker…",
                success: "Restarted",
                error: "The worker exited with status 1",
              })
            }
          >
            Restart, and fail
          </Button>
        </Stack>
      </Section>
    </>
  );
}
