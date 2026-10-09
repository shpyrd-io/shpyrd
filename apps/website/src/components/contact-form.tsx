"use client";

// A contact form: its fields from the content, checked by the forms' rules
// as it is sent, posted to /api/contact. Sent, it gives way to the thanks;
// not sent, it keeps what was typed and offers Discord.
import * as React from "react";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { failed, privacy, technical, thanks, type ContactField } from "@shpyrd/content/site/contact";
import { discord } from "@shpyrd/content/site/offer";
import { check, formOf, trap, type Answers, type Kind } from "@/lib/contact";

type Status = "idle" | "sending" | "sent" | "failed";

// Two fields to a row. A text of several lines takes a whole row, and so
// does a field that would be left alone in its own.
export function wideFields(fields: ContactField[]): Set<string> {
  const wide = new Set<string>();
  const full = (f: ContactField) => f.type === "textarea";
  let half: ContactField | undefined;
  for (const f of fields) {
    if (full(f)) {
      if (half) wide.add(half.name);
      half = undefined;
      wide.add(f.name);
    } else if (half) {
      half = undefined;
    } else {
      half = f;
    }
  }
  if (half) wide.add(half.name);
  return wide;
}

export function ContactForm({ kind }: { kind: Kind }) {
  const form = formOf(kind);
  const [answers, setAnswers] = React.useState<Answers>({});
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const [status, setStatus] = React.useState<Status>("idle");
  const [website, setWebsite] = React.useState("");
  // When the form appeared, on the page's own clock: what is sent is how
  // long the person took, never a time of day.
  const shownAt = React.useRef(0);
  // Until the page's script runs, the form cannot be sent: the browser would
  // send it itself, without the checks.
  const [ready, setReady] = React.useState(false);
  React.useEffect(() => {
    shownAt.current = performance.now();
    setReady(true);
  }, []);

  const set = (name: string, value: string | string[]) => setAnswers((a) => ({ ...a, [name]: value }));
  const text = (name: string) => (typeof answers[name] === "string" ? (answers[name] as string) : "");

  // The first field that needs fixing takes the focus, so its error is
  // what a screen reader says next.
  function show(found: Record<string, string>) {
    setErrors(found);
    const first = form.fields.find((f) => found[f.name]);
    if (!first) return;
    document.getElementById(`contact-${first.name}`)?.focus();
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const found = check(kind, answers);
    show(found);
    if (Object.keys(found).length > 0) return;
    setStatus("sending");
    try {
      const res = await fetch("/api/contact", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ kind, answers, [trap]: website, elapsedMs: Math.round(performance.now() - shownAt.current) }),
      });
      if (res.ok) {
        setStatus("sent");
        (window as unknown as { dataLayer?: unknown[] }).dataLayer?.push({ event: "contact_submitted", form: kind });
        return;
      }
      if (res.status === 400) {
        const body = (await res.json().catch(() => ({}))) as { errors?: Record<string, string> };
        if (body.errors) {
          show(body.errors);
          setStatus("idle");
          return;
        }
      }
      setStatus("failed");
    } catch {
      setStatus("failed");
    }
  }

  if (status === "sent") {
    return (
      <p role="status" className="text-lg">
        {thanks(text("email"))}
      </p>
    );
  }

  const wide = wideFields(form.fields);
  const span = (f: ContactField) => (wide.has(f.name) ? "sm:col-span-2" : undefined);

  const control = (f: ContactField) => {
    const id = `contact-${f.name}`;
    switch (f.type) {
      case "select":
        return (
          <Field key={f.name} id={id} label={f.label} required={f.required} error={errors[f.name]} className={span(f)}>
            <Select name={f.name} value={text(f.name)} onValueChange={(v) => set(f.name, v)}>
              <SelectTrigger
                id={id}
                aria-invalid={errors[f.name] ? true : undefined}
                aria-describedby={errors[f.name] ? `${id}-error` : undefined}
                className="w-full"
              >
                <SelectValue placeholder="Choose…" />
              </SelectTrigger>
              <SelectContent>
                {f.choices?.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        );
      case "textarea":
        return (
          <Field key={f.name} label={f.label} required={f.required} error={errors[f.name]} className={span(f)}>
            <Textarea id={id} name={f.name} className="min-h-32" value={text(f.name)} onChange={(e) => set(f.name, e.target.value)} />
          </Field>
        );
      default:
        return (
          <Field key={f.name} label={f.label} required={f.required} error={errors[f.name]} className={span(f)}>
            <Input
              id={id}
              name={f.name}
              type={f.type}
              autoComplete={f.autoComplete}
              value={text(f.name)}
              onChange={(e) => set(f.name, e.target.value)}
            />
          </Field>
        );
    }
  };

  return (
    <form method="post" noValidate onSubmit={submit} className="grid gap-5 sm:grid-cols-2">
      {status === "failed" && (
        <p role="alert" className="text-sm text-destructive sm:col-span-2">
          {failed} <a href={discord.href} className="underline underline-offset-4">Discord</a>
        </p>
      )}
      {form.fields.map(control)}
      {/* People never see this field; bots fill it in. */}
      <input
        type="text"
        name={trap}
        tabIndex={-1}
        autoComplete="off"
        aria-hidden
        value={website}
        onChange={(e) => setWebsite(e.target.value)}
        className="absolute -left-[9999px] size-px opacity-0"
      />
      <div className="grid gap-3 sm:col-span-2">
        <Button type="submit" disabled={!ready || status === "sending"} className="justify-self-start">
          {form.submit}
        </Button>
        <p className="text-xs text-muted-foreground">
          By submitting, you agree to our{" "}
          <a href={privacy.href} className="underline underline-offset-4">
            {privacy.label}
          </a>
          .
        </p>
        <p className="text-xs text-muted-foreground">
          {technical.text}{" "}
          <a href={discord.href} className="underline underline-offset-4">
            {technical.link}
          </a>
        </p>
      </div>
    </form>
  );
}
