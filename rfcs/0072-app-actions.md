# RFC-0072 App actions: what the platform may do in an app for a person

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0032 (in progress), RFC-0033 (in progress), RFC-0070 (provisional)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

An app declares **actions** — named, typed HTTP endpoints such as `create-invoice` or
`export-report` — and the platform exposes them where its people already are: as tools of
the workspace's MCP server (an assistant does things in the app for the person who asked),
as buttons on the launcher tile, and later to other apps. The platform calls the action
through the edge **as the person**, so the app authorises exactly as it does for a browser:
the identity headers and JWT it already reads, plus a claim saying the platform acted on
their request. No new authentication in apps.

## Motivation

- RFC-0032's connector answers about projects. The natural next question is *"then create
  the invoice"*. Doing that needs a way for an app to say what it can do and for the
  platform to call it safely on someone's behalf — otherwise every app grows its own
  agent API and its own token scheme.
- RFC-0033 named `platform: actions` in the allow list and promised the platform would call
  apps "on behalf of users"; today the entry admits nothing special. The people who build
  small software with AI tools will have their app's actions generated for them the moment
  there is a shape to generate.
- The FDE partner story (Story 2, RFC-0033): consultants deliver apps whose value is what an
  assistant can do with them.

### Goals

- Declaring an action is a few lines in `shpyrd.yaml` (or a manifest the app serves), with
  a JSON schema for its input; the platform validates and documents it.
- An action runs as the person who asked, within their role in the app (`user` acts,
  `reader` may run only actions marked read-only); the app receives the same identity it
  receives for a browser, plus `act.via`.
- Every run is audited: who, which action, from where (assistant, launcher), result.
- Consent: an assistant needs `projects:write` — or a finer `apps:act` scope — and the
  consent page names the apps whose actions it may run.

### Non-Goals

- Workflow engines, schedules (RFC-0024), retries. An action is one call.
- Apps calling other apps' actions autonomously without a person (later; needs RFC-0070's
  service identity and a policy).
- Generating actions for an app: a prompt in the MCP connector's guidance, not this RFC.

## Proposal

- **Declaration** in `shpyrd.yaml`:

  ```yaml
  actions:
    - name: create-invoice
      title: Create an invoice
      description: Creates a draft invoice for a customer.
      method: POST
      path: /actions/create-invoice
      input:            # JSON schema, sent as the request body
        type: object
        properties: { customer: { type: string }, amount: { type: number } }
        required: [customer, amount]
      readOnly: false   # readers may run read-only actions
  ```

  or served by the app at `/.well-known/shpyrd-actions.json` (same shape) for apps that
  generate them at runtime; the controller records them in `status.actions`.
- **Exposure.** The workspace's MCP server (RFC-0032) lists each action as a tool named
  `<project>.<action>` for the people who may run it; the launcher tile shows up to three
  actions as buttons (a small form from the schema). Both call the platform's
  `POST /api/projects/<slug>/actions/<name>` with the input.
- **The call.** The platform calls the app through the edge at its own host
  (`https://<app host><path>`), or internally through RFC-0070's front door, with the
  person's identity: the same headers and JWT as a browser request, plus JWT claims
  `act: {via: "mcp" | "launcher", client: "<client id>"}` so the app can tell (and refuse
  if it wants). The edge decides admission as for any request — the person's role in the
  app; `reader` only for `readOnly` actions.
- **Result.** The app answers JSON (or text); the platform returns it to the caller and
  records `action.run` in the project's audit with the actor and the via. Errors from the
  app (4xx/5xx) are returned as errors with the body's message.
- **Consent.** RFC-0032's scopes gain `apps:act` (run actions in apps I may use); the consent
  page lists it as "Do things in your apps, as you". Without it the tools are not offered.

## Design Details

- Validation: the schema is validated when the App is applied (a `draft-07` subset);
  inputs are validated before the call; `path` must be under the app's own host, `method`
  one of GET/POST/PUT/PATCH/DELETE.
- Idempotency: the platform sends `Idempotency-Key` (the run id) so an app may deduplicate
  retried calls from an assistant.
- Timeouts: 60 s by default (`timeout:` per action up to 300 s); the launcher shows "still
  running" and polls the run record.
- Runs are records (`action_runs`: id, project, action, actor, via, started, finished,
  status, error) with a short retention, listed on the project page.
- Allow lists: the platform's calls come from the server; `platform: actions` becomes a
  real peer (the server's identity) rather than one the base policy already admits
  (RFC-0033's audit gap).

## Open questions

1. `shpyrd.yaml` declaration, the served manifest, or both? Default: **both**, the manifest
   winning when present (apps that generate their own).
2. A dedicated scope `apps:act` or fold into `projects:write`? Default: **dedicated** —
   deploying an app and acting inside it are different powers.
3. Do actions appear on the launcher tile in the first version? Default: **MCP first**,
   launcher buttons second.

## Implementation History

- 2026-09-27: RFC written (research; replaces the reference "RFC-0067 app actions" in
  RFC-0033, whose number went to build profiles).
