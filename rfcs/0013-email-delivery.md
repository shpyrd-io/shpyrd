# RFC-0013 Email delivery

**Status:** implemented (v0.9.13)

**Owner:** Patrick Negri

**Depends on:** RFC-0002 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-09-30

## Summary

A `mail` extension that lets the platform send email over SMTP, to any relay,
with a test command. Used by account lifecycle flows (RFC-0014) and notifications
(RFC-0030).

## Motivation

Invitations, password resets and alerts need a sender; every consumer configuring its own
SMTP would be repetitive and error prone.

### Goals

- One configuration, one sender identity, one test: `shpyrd mail test you@example.com`.
- Credentials never printed; delivery failures visible in the audit trail.

### Non-Goals

- Receiving mail. Templating beyond simple text/HTML with a header and footer.
- Suppression lists and bounce handling: the relay's job, not the platform's (see
  "Suppression and bounces" below).
- HTTP provider adapters (SES API, Resend, Postmark): every commercial provider also
  speaks SMTP, so one adapter covers them all.

## Proposal

- `shpyrd-ctl extensions enable mail`, then `shpyrd-ctl mail set --host smtp.example.com
  --port 587 --user ... --password @file --from "shpyrd <noreply@example.com>"
  [--tls|--plain]` (STARTTLS by default). Everything lives in the Secret `shpyrd-mail` of
  the system namespace, read again every 30 seconds so a change needs no restart;
  `mail status` shows it without the password; `mail unset` removes it.
- `pkg/ext/mail` implements `ext.Mailer` (`Send(ctx, Message{To, Subject, Text, HTML})`,
  `Configured(ctx)`); the server asks the enabled extensions for one (`ext.MailProvider`)
  and hands it to the others through `ext.Deps.Mail`. Deliveries are rate limited per
  recipient (5 in 10 minutes).
- `shpyrd-ctl mail test you@example.com` asks the server (`POST /api/cluster/mail/test`)
  to send a message, so the test proves what invitations will use: the settings, the
  network path from inside the cluster and the sender address.
- Dashboard: the Cluster page's "Email" card shows the status and sends a test message.
  Tests and failures are audited (`mail.test`, `mail.test_failed`); the settings changes
  too (`mail.set`, `mail.unset`).

## Design Details

- Go `net/smtp`: STARTTLS (required when chosen; a server without it is an error naming
  the alternatives), TLS from the first byte, or none for a relay on a private network;
  PLAIN or LOGIN authentication, neither sent unencrypted except to localhost; one
  deadline for the whole delivery (30 s).
- Messages are `multipart/alternative` when HTML is given, quoted-printable, with
  `Message-ID`, `Date`, `Auto-Submitted: auto-generated`. The invitation email is text
  plus a small HTML wrapper with the wordmark; links use the workspace's dashboard URL.
- Consumers so far: invitations (RFC-0033). Notifications (RFC-0030) and account
  lifecycle (RFC-0014) plug into the same `Mailer`.

### Suppression and bounces

The platform keeps no list of addresses it must not write to, and does not learn what
happens to a message after the relay accepts it. Both are deliberate: they belong to the
relay, and the two kinds of relay differ.

- **A commercial relay** (Mailgun, SES, Postmark, Resend, SendGrid...) suppresses on its
  own, per sending domain, on every plan: addresses that bounced hard, complained of spam
  or unsubscribed are added automatically and refused from then on, through SMTP as much
  as through its API; the operator can add or remove addresses in its panel or API. The
  relay also detects bounces, which is how it fills the list. Nothing is left for the
  platform to do, and doing it anyway would keep a second, poorer copy of the same list.
- **A relay of your own** (Postfix, a corporate SMTP) does none of this. The only
  protection is the platform's rate limit (5 messages per recipient in 10 minutes), and a
  bounce goes to the sender's mailbox like any other mail. This fits the platform's
  volume: invitations, resets and alerts to people who were named by an administrator,
  not lists; an unreachable address is the administrator's to notice.

What neither relay gives the platform is the asynchronous outcome: the relay answers
"accepted" and the failure, if any, happens later and is known to the relay alone. So the
audit trail records delivery failures the relay refused synchronously (wrong credentials,
no route, a recipient rejected at the SMTP dialogue) and not those discovered afterwards.
Showing those would take a webhook endpoint per provider, which is a feature of its own
if ever wanted, not a gap of this one.

## Implementation status

Shipped in v0.9.13 as described. Closed on 2026-09-30: the gaps the 2026-09-25 audit
listed are decisions, not debts — HTTP adapters are unnecessary (SMTP reaches every
provider), suppression and bounces are the relay's (above), and the last test result is
recorded in the audit trail, where every other failure is, rather than on the card.

## Open questions

1. SMTP only, or also an HTTP provider (SES API, Resend, Postmark)? Default: SMTP first,
   the `Mailer` interface ready for HTTP providers. (Resolved: SMTP only; every commercial
   provider offers it, so adapters would add code without reach.)
2. Should the platform keep its own suppression list and read bounces? (Resolved: no;
   the relay does it, see "Suppression and bounces".)

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-27: implemented in v0.9.13 (the `mail` extension, `shpyrd-ctl mail`, the Email
  card, invitations emailed).
- 2026-09-30: closed. Suppression and bounces assigned to the relay, HTTP adapters and the
  card's last test result dropped.
