# RFC-0074 SCIM provisioning: people and teams from the company directory

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (in progress: memberships, teams, suspension, SSO), RFC-0058
(implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

A workspace exposes a SCIM 2.0 endpoint that its identity provider (Okta, Microsoft Entra,
Google Workspace, JumpCloud) pushes to: people are created before their first sign-in, with
their teams from directory groups, and **deprovisioned the moment HR removes them** —
suspended at once, sessions ended, tokens stopped — rather than at their session's expiry.
Groups become teams and stay in sync. A licensed feature (`ee/`), per RFC-0033's open-core
boundary; the just-in-time provisioning of RFC-0033 remains the default for everyone else.

## Motivation

- RFC-0033 promised it: *"Deprovisioning: when the IdP refuses the person, access ends at
  session expiry; SCIM (licensed, RFC to come) makes it immediate and adds
  pre-provisioning."* Sessions last up to seven days; a company that fired someone on
  Monday does not want to wait.
- Companies with an identity provider manage people in one place. Inviting them a second
  time in shpyrd (RFC-0033 invitations) is the small-company path; the directory is the
  medium-company path, and the one security reviews ask about.
- Teams from directory groups exist today by name (a team lists the `groups` claim values
  that map to it), recomputed at sign-in; SCIM makes the mapping explicit, complete and
  independent of who signed in recently.

### Goals

- One SCIM base URL and one bearer token per workspace, created on Workspace › Sign-in
  (owners), pasted into the IdP's provisioning settings; `Users` and `Groups` resources with
  the operations Okta and Entra send.
- A pushed user is a person of the workspace with a membership (`member` by default, or the
  role a directory attribute names); `active: false` suspends them immediately, ends their
  sessions and their tokens' effect; deletion suspends and marks them for removal.
- A pushed group is a team; group membership is team membership; the team's IdP-group
  mapping is set so JIT sign-ins agree with SCIM.
- Every change is audited with `via: scim`.

### Non-Goals

- Being the identity provider (passwords, MFA): the IdP stays the source; RFC-0033's login
  methods sign people in.
- SCIM for the console pool or partner staff (later, with partners).
- Attribute mapping beyond email, name, active, groups, and one role attribute.

## Proposal

- **Endpoint** at `https://<workspace>/scim/v2/`: `ServiceProviderConfig`, `Schemas`,
  `ResourceTypes`, `Users` (GET with `filter=userName eq "..."`, POST, PUT, PATCH with
  `replace`/`add`/`remove` on `active`, `name`, `emails`, `groups`; DELETE), `Groups`
  (GET, POST, PUT, PATCH with member `add`/`remove`, DELETE). Bearer token per workspace,
  hashed in the store, rotated from the page; rate limited.
- **Users → people.** `userName`/primary email → identity (created with
  `provider: scim`, `status: active`, in `Everyone`), membership `member` unless the
  `roles` attribute (or a configured custom attribute) says `admin` or `owner` (owners only
  through SCIM when the workspace setting allows it: default no). `active: false` →
  `SetIdentityStatus(suspended)` + sessions of the person deleted + tokens refused (their
  owner is suspended: RFC-0031 already checks); `active: true` reactivates. DELETE →
  suspend and record `deletedAt`; the person is forgotten after 30 days.
- **Groups → teams.** `displayName` → team name (slugified, unique per workspace); members
  → team members (by email); the team's `groups` mapping gets the group's `externalId`/name
  so a JIT sign-in with that group agrees. Teams created by SCIM are marked
  `managedBy: scim`: the Teams page shows the badge and refuses edits that SCIM would
  overwrite.
- **Setting up.** Workspace › Sign-in › Provisioning (owners, licensed): create the token
  (shown once), the base URL, IdP-specific notes (Okta: "SCIM 2.0 with OAuth Bearer Token";
  Entra: "Provisioning mode automatic, tenant URL"). A test button runs the IdP's usual
  first calls.
- **License.** The routes answer 402 "available with a license" without one (RFC-0033
  `Licensing` seam); the page says so.

## Design Details

- Store: `scim_tokens(workspace_id, hash, created_at, last_used_at)`; identities gain
  `external_id` and `deleted_at`; teams gain `managed_by`.
- Conformance: the subset Okta and Entra exercise (RFC 7643/7644), verified with Okta's
  SCIM test suite (Runscope collection) and Entra's provisioning test.
- Immediate deprovisioning: `SetIdentityStatus(suspended)` already stops the edge and the
  API within the cache windows (~50 s worst case, RFC-0033 audit); sessions are deleted
  outright so the dashboard cookie is dead on the next request.
- Conflicts: a person who signed in before being pushed keeps their record (email matches);
  a membership set by hand and a role from SCIM: SCIM wins while active, audited.
- Audit: `scim.user.create|update|deactivate|delete`, `scim.group.*`.

## Open questions

1. Licensed (`ee/`) as RFC-0033's table says, or core? Default: **licensed** — the feature
   pays for the conformance work and is what medium companies pay for.
2. May SCIM set `owner`? Default: **no** by default, a workspace setting to allow.
3. Group → team name collisions with hand-made teams? Default: SCIM takes the name over
   (with a badge), the hand-made team is renamed `<name>-manual`, audited.

## Implementation History

- 2026-09-27: RFC written (research; the "SCIM (licensed, RFC to come)" of RFC-0033).
