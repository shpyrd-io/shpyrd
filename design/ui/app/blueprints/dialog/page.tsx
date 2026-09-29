"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
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
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [confirmed, setConfirmed] = useState("none");
  return (
    <>
      <Section title="Dialog">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="outline">Open</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Destroy the project?</DialogTitle>
                <DialogDescription>
                  Its instances stop and its address is released.
                </DialogDescription>
              </DialogHeader>
            </DialogContent>
          </Dialog>
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="outline">With actions</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Rename the project</DialogTitle>
                <DialogDescription>Its address stays the same.</DialogDescription>
              </DialogHeader>
              <Field label="Name">
                <Input defaultValue="Hello World" />
              </Field>
              <DialogFooter>
                <DialogClose asChild>
                  <Button variant="outline">Cancel</Button>
                </DialogClose>
                <DialogClose asChild>
                  <Button onClick={() => setConfirmed("Save")}>Save</Button>
                </DialogClose>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </Stack>
      </Section>
      <Section title="Confirm dialog: it asks before it acts">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <ConfirmDialog
            trigger={<Button variant="outline">With a question</Button>}
            title="Restart the instances?"
            description="The project is out for a few seconds."
            action="Restart"
            onConfirm={() => setConfirmed("Restart")}
          />
          <ConfirmDialog
            trigger={
              <Button variant="destructive" icon={<Trash2 />}>
                Destroy, typing the name
              </Button>
            }
            variant="destructive"
            title="Destroy the project?"
            description="Its instances stop and its address is released. This cannot be undone."
            confirmation="hello-world"
            action="Destroy the project"
            onConfirm={() => setConfirmed("Destroy")}
          />
        </Stack>
        <p className="text-sm text-muted-foreground">The last one confirmed: {confirmed}</p>
      </Section>
    </>
  );
}
