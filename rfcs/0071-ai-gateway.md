# RFC-0071 AI gateway: model calls through the platform

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0002 (implemented), RFC-0033 (in progress), RFC-0042 (implementable),
RFC-0048 (implementable)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

Apps call language models through the platform instead of holding provider keys: the
workspace's admin configures a provider once (OpenAI, Anthropic, Google, a self-hosted
OpenAI-compatible endpoint), every project gets `AI_GATEWAY_URL` and a project-scoped key,
and the platform proxies the calls: it meters tokens and cost per project, enforces budgets,
records what was spent (never the prompts, by default), and can later apply guardrails. An
`ai-gateway` extension (RFC-0002): the proxy in the core, the cost controls and guardrails
in `ee/` per RFC-0033's open-core boundary.

## Motivation

- The apps this platform is for are built with and around models; each carries a provider
  key pasted into a config var, with no idea of what it costs until the invoice. A
  workspace with ten such apps has ten keys and one surprise.
- The partner story (RFC-0033 Story 2): the consulting shop sells apps that call models on
  the customer's behalf; the customer needs to see the model spend per app, and to cap it.
- Metering is the seam RFC-0033 names for billing (RFC-0042, RFC-0048): model tokens are the
  first usage that is not CPU or memory.

### Goals

- One setting per workspace (provider, key, default model), one config var per project
  (`AI_GATEWAY_URL`, plus `AI_GATEWAY_KEY`), and every OpenAI-compatible client library
  works by changing its base URL — no SDK of ours.
- Cost per project and per workspace, live, on the project page and the plan card; a
  monthly budget per project and per workspace, refused past it with a clear error the app
  can show.
- Prompts and completions are not stored; token counts, model, latency and status are.
- Provider keys never reach an app; a project key opens only the gateway.

### Non-Goals

- Hosting models, fine-tuning, embeddings stores.
- Prompt caching, response caching, semantic routing (later, if at all).
- A model marketplace.

## Proposal

- **Extension `ai-gateway`** with a component: a small Go proxy Deployment
  (`ai-gateway.shpyrd-system`) and a NetworkPolicy admitting project namespaces to it;
  disabled by default.
- **Providers** per workspace: Workspace › AI (owners and admins): provider kind, API key
  (Secret `shpyrd-ai-<workspace>`), default model, optional allowed models. The platform's
  own provider (operator) may be offered to workspaces that set none (metered and billed to
  them; cloud).
- **Project side**: attaching the gateway to a project (like a resource, RFC-0003:
  `shpyrd ai attach shop`) sets `AI_GATEWAY_URL=http://ai-gateway.shpyrd-system/v1` and
  `AI_GATEWAY_KEY=aig_<random>` (a project-scoped key, hashed in the store). The gateway
  maps the key to (workspace, project), picks the workspace's provider, forwards the call.
- **API surface**: OpenAI-compatible (`/v1/chat/completions`, `/v1/embeddings`, streaming
  included) translated to the provider's API when it differs (Anthropic Messages, Google);
  a native Anthropic passthrough (`/anthropic/v1/messages`) for apps that use that SDK.
- **Metering**: per call, `(workspace, project, model, input tokens, output tokens, cost,
  latency, status)` into the control-plane store (a `usage_events` table, RFC-0033's
  `Metering` seam); prices per model in a table the operator maintains; the project page
  gets an "AI" card (calls, tokens, cost, last 24 h / month), the plan card a line.
- **Budgets** (`ee/`): monthly cost cap per project and per workspace; 402 with
  `{"error":"budget exhausted", "resets_at": ...}` past it; alerts through RFC-0030 at 80 %.
- **Guardrails** (`ee/`, later): allowed models, max tokens per call, PII redaction in
  prompts, an audit of prompts when the workspace opts in.

## Design Details

- The proxy is stateless; it caches provider keys and project keys for a minute from the
  store. Streaming responses are relayed as they arrive; token counts come from the
  provider's usage fields (or a tokenizer estimate when absent, marked as such).
- Costs: a `model_prices` table (provider, model, input/output price per million tokens,
  valid from) seeded from the providers' public lists; the operator edits.
- Failure modes: provider down → 502 with the provider's message; key missing → 401 to the
  app naming the setting to fix; the gateway itself is not on any app's critical path
  unless the app uses it.
- Security: the project key is a bearer valid only at the gateway; the provider key lives in
  one Secret per workspace read by the gateway alone; the NetworkPolicy admits only project
  namespaces and the server.

## Open questions

1. Core/`ee/` line: proxy, metering and the per-project view in core; budgets, alerts and
   guardrails in `ee/`? Default: **yes**, as RFC-0033's table says.
2. Offer the platform's own provider to workspaces without one (cloud, billed)? Default:
   **later**, once billing exists (RFC-0033 phase 8).
3. Store prompts when the workspace opts in (debugging, audits)? Default: **no** in the
   first version; opt-in in `ee/` guardrails.

## Implementation History

- 2026-09-27: RFC written (research; replaces the reference "RFC-0066 AI gateway" in
  RFC-0033, whose number went to the release phase).
