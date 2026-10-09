"use client";

// A contact form: its fields from the content, checked by the forms' rules
// as it is sent, posted to /api/contact. Sent, it gives way to the thanks;
// not sent, it keeps what was typed and offers Discord.
import * as React from "react";
import { Button } from "@shpyrd/ui/components/button";
import { Checkbox } from "@shpyrd/ui/components/checkbox";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { failed, privacy, technical, thanks, type ContactField } from "@shpyrd/content/site/contact";
import { discord } from "@shpyrd/content/site/offer";
import { check, formOf, trap, type Answers, type Kind } from "@/lib/contact";

type Status = "idle" | "sending" | "sent" | "failed";

export function ContactForm({ kind }: { kind: Kind }) {
  const form = formOf(kind);
  const [answers, setAnswers] = React.useState<Answers>({});
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const [status, setStatus] = React.useState<Status>("idle");
  const [website, setWebsite] = React.useState("");
  const startedAt = React.useRef(0);
  React.useEffect(() => {
    startedAt.current = Date.now();
  }, []);

  const set = (name: string, value: string | string[]) => setAnswers((a) => ({ ...a, [name]: value }));
  const text = (name: string) => (typeof answers[name] === "string" ? (answers[name] as string) : "");
  const list = (name: string) => (Array.isArray(answers[name]) ? (answers[name] as string[]) : []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const found = check(kind, answers);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    setStatus("sending");
    try {
      const res = await fetch("/api/contact", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ kind, answers, [trap]: website, startedAt: startedAt.current }),
      });
      if (res.ok) {
        setStatus("sent");
        (window as unknown as { dataLayer?: unknown[] }).dataLayer?.push({ event: "contact_submitted", form: kind });
        return;
      }
      if (res.status === 400) {
        const body = (await res.json().catch(() => ({}))) as { errors?: Record<string, string> };
        if (body.errors) {
          setErrors(body.errors);
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

  const control = (f: ContactField) => {
    const id = `contact-${f.name}`;
    switch (f.type) {
      case "select":
        return (
          <Field key={f.name} id={id} label={f.label} required={f.required} error={errors[f.name]}>
            <Select name={f.name} value={text(f.name)} onValueChange={(v) => set(f.name, v)}>
              <SelectTrigger id={id} aria-invalid={errors[f.name] ? true : undefined} className="w-full">
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
      case "checkboxes":
        return (
          <fieldset key={f.name} className="grid gap-3 sm:col-span-2">
            <legend className="mb-1 text-sm font-medium">{f.label}</legend>
            {f.choices?.map((c) => {
              const cid = `${id}-${c.value}`;
              const chosen = list(f.name);
              return (
                <div key={c.value} className="flex items-center gap-2.5">
                  <Checkbox
                    id={cid}
                    checked={chosen.includes(c.value)}
                    onCheckedChange={(on) =>
                      set(f.name, on === true ? [...chosen, c.value] : chosen.filter((v) => v !== c.value))
                    }
                  />
                  <Label htmlFor={cid}>{c.label}</Label>
                </div>
              );
            })}
            {errors[f.name] && <p className="text-xs text-destructive">{errors[f.name]}</p>}
          </fieldset>
        );
      case "textarea":
        return (
          <Field key={f.name} label={f.label} error={errors[f.name]} className="sm:col-span-2">
            <Textarea id={id} name={f.name} rows={4} value={text(f.name)} onChange={(e) => set(f.name, e.target.value)} />
          </Field>
        );
      default:
        return (
          <Field key={f.name} label={f.label} required={f.required} error={errors[f.name]}>
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
    <form noValidate onSubmit={submit} className="grid gap-5 sm:grid-cols-2">
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
        <Button type="submit" disabled={status === "sending"} className="justify-self-start">
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
