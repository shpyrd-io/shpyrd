# RFC-0013 Email delivery

**Status:** implemented (v0.9.13; gaps listed)

**Owner:** Patrick Negri

**Depends on:** RFC-0002 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-09-27

## Summary

A `mail` extension that lets the platform send email (SMTP, optionally an HTTP provider),
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

## Implementation status

Shipped in v0.9.13 as described. Gaps: no HTTP provider adapters (SES API, Resend,
Postmark) yet, SMTP only; no per-recipient suppression list or bounce handling; the last
test result is in the audit trail, not on the card.

## Open questions

1. SMTP only, or also an HTTP provider (SES API, Resend, Postmark)? Default: SMTP first,
   the `Mailer` interface ready for HTTP providers. (Resolved so: SMTP shipped.)

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-27: implemented in v0.9.13 (the `mail` extension, `shpyrd-ctl mail`, the Email
  card, invitations emailed).
