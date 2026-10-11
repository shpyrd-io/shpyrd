import type { ReactNode } from "react";
import { Button, Email, InvitationEmail, Facts, Fallback, Heading, Small, Strong, Text } from "./parts";

// The emails the platform sends. Each is drawn from its words: built, a
// word is a Go template's mark ({{.Name}}), filled by pkg/emails; in the
// development server, a sample.
type Words = Record<string, string>;

export type Mail = {
  title: string;
  // Which shpyrd sends it: every platform, or shpyrd cloud alone.
  group: "Platform" | "Cloud";
  // Who sends it, and when.
  about: string;
  subject: string;
  sample: Words;
  render: (w: Words) => ReactNode;
};

// The footer of the platform's own mail, sent from the door the person
// uses (acme.shpyrd.app, or a self-hosted platform's address).
function sentFrom(w: Words, why: ReactNode) {
  return (
    <>
      {why}
      <br />
      Sent by shpyrd at {w.Door}.
    </>
  );
}

const invitation = {
  Logo: "/logo-full.png",
  Inviter: "Ana Ribeiro (ana@acme.com)",
  Workspace: "Acme",
  What: "as a developer, in team platform",
  Email: "joao@acme.com",
  Door: "acme.shpyrd.app",
  Link: "https://acme.shpyrd.app/invite/7f3a9c2e",
  Until: "Oct 17, 2026",
};

export const mails: Record<string, Mail> = {
  invite: {
    title: "Invitation",
    group: "Platform",
    about: "A person is invited to a workspace that signs in through a provider (pkg/api/invitations.go).",
    subject: "{{.Inviter}} invited you to {{.Workspace}}",
    sample: invitation,
    render: (w) => (
      <InvitationEmail
        preview={`Join ${w.Workspace} on shpyrd ${w.What}.`}
        logo={w.Logo}
      >
        <Heading>Join {w.Workspace} on shpyrd</Heading>
        <Text>
          <Strong>{w.Inviter}</Strong> invited you to join <Strong>{w.Workspace}</Strong> {w.What}.
        </Text>
        <Text>Accept by opening the invitation and signing in as <Strong>{w.Email}</Strong>.</Text>
        <Button href={w.Link}>Accept invitation</Button>
        <Fallback href={w.Link} />
        <Small>The invitation works until {w.Until}. If you were not expecting it, you can ignore this email.</Small>
      </InvitationEmail>
    ),
  },
  notice: {
    title: "Workspace notice",
    group: "Cloud",
    about: "The platform tells the owners of a workspace what happened to it: near its monthly limit, paused, back (shpyrd-cloud, pkg/cloud/budget.go).",
    subject: "{{.Title}}",
    sample: {
      Title: "Acme is paused until Nov 1",
      Text: "Your workspace Acme reached its plan's monthly limit. It is paused until Nov 1, the first day of next month; its apps answer nothing until then.",
      Action: "See the usage",
      Link: "https://acme.shpyrd.app/workspace/billing",
      More: "Write to us to move to a plan without a limit.",
      Door: "acme.shpyrd.app",
      Workspace: "Acme",
    },
    render: (w) => (
      <Email preview={w.Text} logo={w.Logo} footer={sentFrom(w, <>You got this because you own {w.Workspace}.</>)}>
        <Heading>{w.Title}</Heading>
        <Text>{w.Text}</Text>
        <Button href={w.Link}>{w.Action}</Button>
        <Small>{w.More}</Small>
      </Email>
    ),
  },
  test: {
    title: "Test message",
    group: "Platform",
    about: "An administrator checks that the platform's mail is sent (pkg/ext/mail/extension.go).",
    subject: "shpyrd test message",
    sample: { Host: "smtp.postmarkapp.com", Port: "587", Security: "STARTTLS", From: "shpyrd@acme.com", Sent: "Sat, 03 Oct 2026 14:32:07 UTC", Door: "acme.shpyrd.app" },
    render: (w) => (
      <Email preview="Email delivery works." logo={w.Logo} footer={sentFrom(w, <>You got this because an administrator sent a test message to this address.</>)}>
        <Heading>Email delivery works</Heading>
        <Text>This is a test message from your shpyrd platform. If you can read it, the platform can send email.</Text>
        <Facts
          rows={[
            ["Server", `${w.Host}:${w.Port}`],
            ["Security", w.Security],
            ["From", w.From],
            ["Sent", w.Sent],
          ]}
        />
      </Email>
    ),
  },
};

// The words as a Go template's marks: {{.Name}} for each.
export function marks(mail: Mail): Words {
  const w: Words = { Logo: "{{.Logo}}" };
  for (const k of Object.keys(mail.sample)) w[k] = `{{.${k}}}`;
  return w;
}

export function sample(mail: Mail): Words {
  return { Logo: "/logo.png", ...mail.sample };
}

// The subject with sample words.
export function subjectOf(mail: Mail): string {
  return mail.subject.replace(/\{\{\.(\w+)\}\}/g, (_, k: string) => mail.sample[k] ?? k);
}

export const groups = ["Platform", "Cloud"] as const;
