import type { ReactNode } from "react";
import { Button, Code, Email, Facts, Fallback, Heading, Small, Strong, Text } from "./parts";

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
      <Email
        preview={`Join ${w.Workspace} on shpyrd ${w.What}.`}
        logo={w.Logo}
        footer={sentFrom(w, <>You got this because {w.Inviter} invited {w.Email}.</>)}
      >
        <Heading>Join {w.Workspace} on shpyrd</Heading>
        <Text>
          <Strong>{w.Inviter}</Strong> invited you to join <Strong>{w.Workspace}</Strong> {w.What}.
        </Text>
        <Text>Accept by opening the invitation and signing in as <Strong>{w.Email}</Strong>.</Text>
        <Button href={w.Link}>Accept invitation</Button>
        <Fallback href={w.Link} />
        <Small>The invitation works until {w.Until}. If you were not expecting it, you can ignore this email.</Small>
      </Email>
    ),
  },
  "invite-password": {
    title: "Invitation with a password",
    group: "Platform",
    about: "A person is invited to a workspace with password sign-in, and chooses one (pkg/api/invitations.go).",
    subject: "{{.Inviter}} invited you to {{.Workspace}}",
    sample: { ...invitation, SetLink: "https://acme.shpyrd.app/account/set-password?token=b81d40" },
    render: (w) => (
      <Email
        preview={`Join ${w.Workspace} on shpyrd ${w.What}.`}
        logo={w.Logo}
        footer={sentFrom(w, <>You got this because {w.Inviter} invited {w.Email}.</>)}
      >
        <Heading>Join {w.Workspace} on shpyrd</Heading>
        <Text>
          <Strong>{w.Inviter}</Strong> invited you to join <Strong>{w.Workspace}</Strong> {w.What}.
        </Text>
        <Text>
          Choose a password to get in. You will then sign in at <Strong>{w.Door}</Strong> as <Strong>{w.Email}</Strong>.
        </Text>
        <Button href={w.SetLink}>Choose a password</Button>
        <Fallback href={w.SetLink} />
        <Small>
          This link works for 24 hours. Prefer another way?{" "}
          <a href={w.Link} style={{ color: "inherit" }}>
            Open the invitation
          </a>{" "}
          and sign in with a method {w.Workspace} offers; it works until {w.Until}. If you were not expecting it, you can ignore this email.
        </Small>
      </Email>
    ),
  },
  reset: {
    title: "Password reset",
    group: "Platform",
    about: "A person asks for a new password at the sign-in page (pkg/api/account.go).",
    subject: "Reset your password",
    sample: { Email: "joao@acme.com", Door: "acme.shpyrd.app", Link: "https://acme.shpyrd.app/account/reset?token=c90e11" },
    render: (w) => (
      <Email preview="Choose a new password. The link works for one hour." logo={w.Logo} footer={sentFrom(w, <>You got this because a new password was asked for {w.Email}.</>)}>
        <Heading>Reset your password</Heading>
        <Text>
          Someone asked to reset the password of <Strong>{w.Email}</Strong> at {w.Door}. Choose a new one here:
        </Text>
        <Button href={w.Link}>Choose a new password</Button>
        <Fallback href={w.Link} />
        <Small>The link works for one hour. If you did not ask for it, you can ignore this email: your password stays as it is.</Small>
      </Email>
    ),
  },
  "password-changed": {
    title: "Password changed",
    group: "Platform",
    about: "A person's password was changed, after a reset (pkg/api/account.go).",
    subject: "Your password was changed",
    sample: { Email: "joao@acme.com", Door: "acme.shpyrd.app", When: "Oct 3, 2026 at 14:32 UTC" },
    render: (w) => (
      <Email preview={`The password of ${w.Email} was changed.`} logo={w.Logo} footer={sentFrom(w, <>You got this because the password of {w.Email} changed.</>)}>
        <Heading>Your password was changed</Heading>
        <Text>
          The password of <Strong>{w.Email}</Strong> at {w.Door} was changed on {w.When}, and every session was signed out.
        </Text>
        <Text style={{ margin: 0 }}>If it was you, there is nothing to do.</Text>
        <Small>If it was not you, tell your workspace&apos;s administrator now: someone may have access to your email.</Small>
      </Email>
    ),
  },
  code: {
    title: "Sign-up code",
    group: "Cloud",
    about: "A person signs up to shpyrd cloud and proves the address (shpyrd-cloud, pkg/cloud/signup.go).",
    subject: "Your shpyrd code: {{.Code}}",
    sample: { Code: "482913", Email: "joao@acme.com", Door: "shpyrd.io" },
    render: (w) => (
      <Email preview={`${w.Code} is your shpyrd code.`} logo={w.Logo} footer={sentFrom(w, <>You got this because {w.Email} was given to sign up to shpyrd.</>)}>
        <Heading>Your sign-up code</Heading>
        <Text>Type this code where you signed up to confirm your email:</Text>
        <Code>{w.Code}</Code>
        <Small>The code works for ten minutes. If you did not ask for it, you can ignore this email.</Small>
      </Email>
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
