import type { ReactNode } from "react";
import { Check, CircleCheck, CircleX, Clock, Lock, Smartphone, TriangleAlert, Undo2 } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { StatePage } from "@shpyrd/ui/components/state-page";
import type { Fill, ServerPage } from "./pages";

// The pages Dex serves itself at the sign-in host, drawn as its templates
// (web/templates of Dex v2.45): the same words Dex gives each one
// ({{.ErrMsg}}, {{.PostURL}}), its own functions for the rest (url, extra,
// lower), and sprig's. scripts/pack.mjs writes them into the ConfigMap the
// Dex Deployment lays over the image's (deploy/components/dex/base).
//
// Most people never see one: the platform names the connector, Dex sends
// the browser straight on. They show when a sign-in goes wrong, when Dex
// has to ask which way to sign in, and for the password of an OSS install.

// Where to start again, when the operator said (frontend.extra.home in the
// Dex config): the dashboard, never a place the request names.
function restart(f: Fill, label = "Start again") {
  return f.when(
    'extra "home"',
    "Home",
    <Button asChild size="lg">
      <a href={f.action('extra "home"', "#")}>{label}</a>
    </Button>,
  );
}

// What went wrong, from Dex's words: a sign-in that is gone (an old tab, a
// link used twice), one the identity provider refused, a person outside
// the groups it is for, one cancelled, or something else.
const kinds = [
  `$kind := "other"`,
  `if or (contains "does not exist" .ErrMsg) (contains "session" .ErrMsg) (contains "xpired" .ErrMsg) (contains "Invalid request" .ErrMsg)`,
  `$kind = "expired"`,
  `else if or (contains "Authentication failed" .ErrMsg) (contains "Login error" .ErrMsg) (contains "Connector Login Error" .ErrMsg)`,
  `$kind = "failed"`,
  `else if contains "required groups" .ErrMsg`,
  `$kind = "denied"`,
  `else if contains "rejected" .ErrMsg`,
  `$kind = "cancelled"`,
  `end`,
];

function errorPage(f: Fill): ReactNode {
  const say = (title: string, text: string) => ({ title, text });
  const words = {
    expired: say("This sign-in has expired", "Sign-in links work once, and only for a short while. Start again from the app or workspace you were signing in to."),
    failed: say("Sign-in didn't go through", "Your identity provider couldn't confirm who you are. Try again; if it keeps happening, ask your administrator."),
    denied: say("This account can't sign in here", "It isn't in a group this sign-in is for. Ask your administrator for access, or sign in with another account."),
    cancelled: say("Sign-in cancelled", "Nothing was shared. Start again whenever you're ready."),
  };
  const other = say("Something went wrong", "We couldn't finish signing you in. Try again in a moment; if it keeps happening, ask your administrator.");
  const icons = { expired: <Clock />, failed: <CircleX />, denied: <Lock />, cancelled: <Undo2 /> };
  return (
    <>
      {kinds.map((k) => f.action(k, "")).join("")}
      <StatePage
        icon={f.choose("$kind", icons, <TriangleAlert />) as React.ReactElement}
        title={f.choose("$kind", Object.fromEntries(Object.entries(words).map(([k, w]) => [k, w.title])), other.title)}
        description={f.choose("$kind", Object.fromEntries(Object.entries(words).map(([k, w]) => [k, w.text])), other.text)}
        action={restart(f)}
      >
        {f.choose(
          "$kind",
          { expired: null, failed: null, denied: null, cancelled: null },
          <p className="max-w-md font-mono text-xs text-balance text-muted-foreground">
            {f.word("ErrType")}: {f.word("ErrMsg")}
          </p>,
        )}
      </StatePage>
    </>
  );
}

// A page of a form that stands alone: the mark over the words, the form
// under them.
function form(title: string, description: ReactNode, body: ReactNode) {
  return (
    <StatePage className="py-20" title={title} description={description}>
      <div className="grid w-full max-w-sm gap-6 text-left">{body}</div>
    </StatePage>
  );
}

const error = (kind: string, ErrType: string, ErrMsg: string) => ({ $kind: kind, ErrType, ErrMsg, Home: "yes" });

export const dex: Record<string, ServerPage> = {
  "dex-error": {
    title: "Sign-in expired",
    group: "dex",
    about: "Dex's error page (error.html) for a sign-in that is gone: an old tab, a link used twice, a missing state. Start again goes to the dashboard (frontend.extra.home).",
    sample: error("expired", "Bad Request", "Requested resource does not exist."),
    render: errorPage,
  },
  "dex-error-failed": {
    title: "Sign-in failed",
    group: "dex",
    of: "dex-error",
    about: "The identity provider refused or failed the sign-in (Dex: Authentication failed, Login error).",
    sample: error("failed", "Internal Server Error", "Authentication failed. Please contact your administrator or try again later."),
    render: errorPage,
  },
  "dex-error-denied": {
    title: "Not in the groups",
    group: "dex",
    of: "dex-error",
    about: "The person is not in the groups the connector requires (403).",
    sample: error("denied", "Forbidden", "You are not a member of any of the required groups to authenticate."),
    render: errorPage,
  },
  "dex-error-other": {
    title: "Sign-in error",
    group: "dex",
    of: "dex-error",
    about: "Anything else Dex refuses (a bad request, a database error): Dex's own words under ours, for whoever looks into it.",
    sample: error("other", "Bad Request", "Unregistered redirect_uri."),
    render: errorPage,
  },
  "dex-login": {
    title: "Choose how to sign in",
    group: "dex",
    about: "Dex's connector chooser (login.html), when the request names no connector and there are several.",
    sample: {
      Connectors: [
        { URL: "#", Name: "GitHub", Type: "github" },
        { URL: "#", Name: "Google", Type: "google" },
        { URL: "#", Name: "Email", Type: "local" },
      ],
    },
    render: (f) =>
      form(
        "Sign in to shpyrd",
        "Choose how to sign in.",
        <div className="grid gap-2">
          {f.each("Connectors", (c) => (
            <Button asChild variant="outline" size="lg" className="w-full">
              <a href={c.URL}>
                {f.when(
                  'has .Type (list "github" "gitlab" "google" "microsoft" "bitbucket" "linkedin" "gitea" "ldap" "saml" "oidc" "keystone")',
                  "",
                  <img src={f.action('url $.ReqPath (printf "static/img/%s-icon.svg" .Type)', "")} alt="" className="size-4" />,
                )}
                Continue with {c.Name}
              </a>
            </Button>
          ))}
        </div>,
      ),
  },
  "dex-password": {
    title: "Password",
    group: "dex",
    about: "Dex's password page (password.html), for the local accounts of an OSS install. Cloud has none: no connector of it asks for a password.",
    sample: { PostURL: "#", Username: "", UsernamePrompt: "Email Address", Invalid: "", BackLink: "#" },
    render: (f) =>
      form(
        "Sign in to shpyrd",
        "With the email and password of your account.",
        <>
          <form method="post" action={f.word("PostURL")} className="grid gap-4">
            <Field label={f.word("UsernamePrompt")}>
              <Input id="login" name="login" type="text" autoComplete="username" required size="lg" defaultValue={f.word("Username")} />
            </Field>
            <Field label="Password">
              <Input id="password" name="password" type="password" autoComplete="current-password" required size="lg" />
            </Field>
            {f.when(
              ".Invalid",
              "Invalid",
              <p id="login-error" role="alert" className="text-sm text-destructive">
                Wrong {f.action("lower .UsernamePrompt", "email address")} or password.
              </p>,
            )}
            <Button type="submit" size="lg" className="w-full">
              Sign in
            </Button>
          </form>
          {f.when(
            ".BackLink",
            "BackLink",
            <p className="text-center text-sm">
              <a href={f.word("BackLink")} className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
                Choose another way to sign in
              </a>
            </p>,
          )}
        </>,
      ),
  },
  "dex-password-invalid": {
    title: "Wrong password",
    group: "dex",
    of: "dex-password",
    about: "The password page after a wrong email or password.",
    sample: { PostURL: "#", Username: "ana@acme.com", UsernamePrompt: "Email Address", Invalid: "yes", BackLink: "#" },
    render: (f) => dex["dex-password"].render(f),
  },
  "dex-approval": {
    title: "Grant access",
    group: "dex",
    about: "Dex's approval page (approval.html). Off: the config skips it (skipApprovalScreen); here for a connector or client that turns it on.",
    sample: { Client: "shpyrd", User: "ana@acme.com", AuthReqID: "x", Scopes: [{ "": "View basic profile information" }, { "": "View your email address" }] },
    render: (f) => (
      <StatePage
        className="py-20"
        picture={<LogoMark className="size-16" />}
        title={`Allow ${f.word("Client")}?`}
        description={
          <>
            It will sign you in as <strong className="font-medium text-foreground">{f.word("User")}</strong>.
          </>
        }
      >
        <div className="grid w-full max-w-sm gap-6 text-left">
          {f.when(
            ".Scopes",
            "Scopes",
            <div className="grid gap-2.5">
              <p className="text-sm font-medium">It will be able to</p>
              <ul className="grid gap-2 text-sm text-muted-foreground">
                {f.each("Scopes", (scope) => (
                  <li className="flex items-start gap-2.5">
                    <Check className="mt-0.5 size-4 shrink-0 text-primary" />
                    <span>{scope[""]}</span>
                  </li>
                ))}
              </ul>
            </div>,
          )}
          <div className="grid gap-2">
            <form method="post">
              <input type="hidden" name="req" value={f.word("AuthReqID")} />
              <input type="hidden" name="approval" value="approve" />
              <Button type="submit" size="lg" className="w-full">
                Allow
              </Button>
            </form>
            <form method="post">
              <input type="hidden" name="req" value={f.word("AuthReqID")} />
              <input type="hidden" name="approval" value="rejected" />
              <Button type="submit" variant="ghost" size="lg" className="w-full">
                Cancel
              </Button>
            </form>
          </div>
        </div>
      </StatePage>
    ),
  },
  "dex-device": {
    title: "Device code",
    group: "dex",
    about: "Dex's device code page (device.html), for a client that signs in with the device flow. The CLI uses the server's own (cli-activate).",
    sample: { PostURL: "#", UserCode: "", Invalid: "" },
    render: (f) =>
      form(
        "Enter the code from your device",
        "It is shown on the device you are signing in on.",
        <form method="post" action={f.word("PostURL")} className="grid gap-3">
          <Field label="Code">
            <Input id="user_code" name="user_code" defaultValue={f.word("UserCode")} autoComplete="off" spellCheck={false} autoCapitalize="characters" placeholder="XXXX-XXXX" required size="lg" className="text-center font-mono uppercase tracking-widest" />
          </Field>
          {f.when(
            ".Invalid",
            "Invalid",
            <p role="alert" className="text-sm text-destructive">
              That code is wrong or has expired.
            </p>,
          )}
          <Button type="submit" size="lg" className="w-full">
            Continue
          </Button>
        </form>,
      ),
  },
  "dex-device-success": {
    title: "Device signed in",
    group: "dex",
    about: "After a device code is accepted (device_success.html).",
    sample: { ClientName: "the CLI" },
    render: (f) => (
      <StatePage
        className="py-20"
        picture={
          <div className="relative">
            <span className="inline-flex size-16 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
              <Smartphone className="size-8" />
            </span>
            <span className="absolute -right-2 -bottom-2 rounded-full bg-background p-1">
              <CircleCheck className="size-6 text-success" />
            </span>
          </div>
        }
        title="You're signed in"
        description={`Go back to ${f.word("ClientName")} to continue. You can close this tab.`}
      />
    ),
  },
  "dex-oob": {
    title: "Copy the code",
    group: "dex",
    about: "Dex's out-of-band page (oob.html), for a client that asks for the code to be pasted by hand. No client of the platform does.",
    sample: { Code: "4f2c9a1b7e" },
    render: (f) =>
      form(
        "Copy this code",
        "Paste it into the application that asked you to sign in.",
        <Field label="Code" labelHidden>
          <Input id="code" readOnly defaultValue={f.word("Code")} size="lg" className="text-center font-mono" />
        </Field>,
      ),
  },
};
