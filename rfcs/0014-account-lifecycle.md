# RFC-0014 Account lifecycle

**Status:** implemented (v0.9.45)

**Owner:** unassigned

**Depends on:** RFC-0012, RFC-0013

**Creation date:** 2026-09-22

**Last update:** 2026-09-28 (implemented)

## Summary

Self-service for local accounts (RFC-0007 step 3.2): invitations, email verification,
password reset, lockout after repeated failures. Built on Dex's local accounts with flows
owned by shpyrd; Dex itself offers none of these.

> Note (2026-09-27): "invite by email" shipped as workspace invitations with RFC-0033 in
> v0.9.13 (`shpyrd invite`, the People tab, `/invite/<token>`, emailed through RFC-0013):
> the invited person joins with a workspace role through whatever sign-in method gives
> their address. What remains here is specific to local accounts: setting a password from
> an invitation, password reset, verification and lockout.

## Motivation

Admins set passwords by hand today and pass them along. Teams need "invite by email" and
"I forgot my password".

### Goals

- `shpyrd users invite ada@example.com` / dashboard "Invite" sends a link; the person sets
  their own password.
- "Forgot password" on the login page.
- Verified emails; lockout with a clear message and a timed release.

### Non-Goals

- MFA/TOTP (a follow-up RFC once this lands).
- Accounts from external providers (their lifecycle belongs to the provider).

## Proposal

- **Signed links**: the server issues single-use tokens (random, hashed in a short-lived
  Secret like login tickets, 24h for invites, 1h for resets) and serves two small pages:
  `/account/set-password?token=...` (invite) and `/account/reset?token=...`. Completing the
  form creates or updates the Dex Password object (`pkg/ext/authlocal.Store`) and marks
  `emailVerified: true`.
- **Invite**: `POST /api/users/invite {email, name}` (platform admins) or a Users page
  action; the account exists in a "pending" state (a Password object with an unusable hash
  and an `invited` annotation) until the link is used; re-invite regenerates the link.
- **Reset**: public `POST /api/auth/reset {email}` (always answers "if the account exists,
  an email was sent"; rate limited); the link sets a new password and ends existing
  sessions of that user.
- **Lockout**: the password endpoint (RFC-0012) counts failures per account; after 10 in
  15 minutes the account is locked for 15 minutes (login says so); audited.
- Users page shows verified/pending/locked.

## Design Details

- Tokens: 32 random bytes, SHA-256 stored, label `shpyrd.io/account-token`, expiry field.
- Email templates: invite, reset, "your password was changed".
- Audit actions: `user.invite`, `user.reset_requested`, `user.reset`, `user.locked`.

## Open questions

1. MFA in this RFC or the follow-up? Default: follow-up (keeps this at ~2 days).

## Implementation History

- 2026-09-22: RFC written.

## Implementation status

v0.9.45:

| Part | Status |
| --- | --- |
| `authlocal.Store`: `CreatePending` (unusable hash, `invited` annotation), `ActivateFromInvite`, `SetPasswordAndVerify` (unlocks), `IsLocked`, `Lock`, `MarkVerified`; `User.Status` (active/pending/locked) and `Verified` | done |
| `tokens.go`: `MintAccountToken`/`RedeemAccountToken`; `TokenKindReset` (1 h), `TokenKindInvite` (24 h); revoke-on-re-mint | done |
| `ext.LocalAccountStore` interface; extension wires the store into the server via `SetLocalAccounts` | done |
| `POST /api/auth/reset`: public, rate-limited (3/min per IP), always 200; `GET`+`POST /account/reset`: inline HTML reset form; `GET`+`POST /account/set-password`: inline HTML invite activation form | done |
| `authPassword` lockout: checks `IsLocked` before the rate limiter; after 10 in-memory failures calls `Lock` for 15 min; successful sign-in clears the lock; `user.locked` audit event | done |
| `shpyrd invite` hook: also calls `inviteUser` when auth-local is enabled, creating a pending account and sending the set-password link alongside the workspace invitation | done |
| `shpyrd-ctl users add --invite`: creates a pending account; `shpyrd-ctl users list` shows STATUS and VERIFIED columns | done |
| Deployed on OKE (v0.9.45): reset endpoint and pages verified | applied |

Known gaps (not started):

- **Email templates** are plain text + minimal HTML; branded templates are a follow-up.
- **`/account/reset` link in the sign-in UI**: the login page shows no "Forgot password" link yet — a UI change deferred until the React app is touched.
- **Verified email enforcement**: accounts marked `invited` (pending) can try to sign in but will fail (unusable hash); the lockout message and the "resend invite" flow are not surfaced in the dashboard.
- **MFA/TOTP**: explicitly out of scope (RFC-0014 §Non-Goals); follow-up RFC.
- **Rate limit persists only in memory**: a restart clears in-memory failure counts; the durable `Lock` annotation survives restarts.

## History

- 2026-09-22: RFC written.
- 2026-09-28 (v0.9.45): implemented.
