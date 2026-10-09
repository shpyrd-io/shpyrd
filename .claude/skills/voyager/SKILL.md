---
name: voyager
description: Create and maintain Voyager product journeys and scenarios (.voyager/) for this repository. Use when asked to add, fix or update Voyager journeys, scenarios, seed scripts or the product map, to cover a feature with an end-to-end journey, or when a `voyager test` run fails.
---

# Authoring Voyager journeys and scenarios

Voyager runs **product journeys**: YAML descriptions of what a user can do in
this product. The same journey powers end-to-end tests (`voyager test`),
screenshots (`voyager capture`) and demo videos (`voyager demo`). This
repository owns the journeys and the data. Voyager owns how they run.

Your job is to produce journeys and scenarios that **actually pass, every
time**. Writing YAML that validates is not enough.

## The rule

A scenario or journey is done only when:

1. `npx voyager validate` passes, **and**
2. `npx voyager test <journey> --no-replay` passes, **and**
3. it passes a **second** time, run immediately after the first (this proves
   the scenario resets the data and nothing leaks between runs).

If you cannot get there, stop and report what blocks you. Never weaken a
`verify` step or delete a failing step to make a run pass.

## Files

```text
.voyager/
├── voyager.yaml          how to start, health-check, seed and browse the app
├── product.yaml          product areas → code globs (used by `voyager update`)
├── scenarios/<id>.yaml   deterministic starting states
└── journeys/<area>/<name>.yaml
```

Journeys may live anywhere under `journeys/`; the folder does not have to
match `area`, but `journeys/<area>/<name>.yaml` is the convention. A journey is
selected by its `id`, its path (`applications/create`) or a folder
(`applications`). Every journey must name a `scenario`.

Run `npx voyager validate` after every edit. It reports every problem with
file and path. Make sure `.voyager-output/` is in `.gitignore`; it holds run
artifacts and is never committed.

## Step 1: understand the app before writing anything

- Find how the app is started, and which URL tells you it is ready.
  Configure `environment.start`, `environment.health` and `environment.base_url`.
- **HTTPS with a self-signed or local CA certificate:** the health check and
  browser reject it by default. Prefer trusting the CA:
  `NODE_EXTRA_CA_CERTS=/path/to/ca.pem npx voyager test` (covers the health
  check). For local development only, set `environment.ignore_https_errors:
  true`, which covers both the health check and the browser.
- Find how the app **already** creates and resets data: migrations, seed
  scripts, fixtures, factories used by its own tests, admin commands. Reuse
  them. Do not build a parallel data layer.
- Find how users log in, and which test account can be used.
- Open the relevant UI code so you know the real page paths and visible texts.

## Step 2: scenarios (deterministic data)

A scenario is a named starting state, created by a command that **this
repository** owns. Voyager runs the command before every journey that uses
the scenario.

```yaml
# .voyager/scenarios/established-team.yaml
id: established-team
description: >
  A workspace "Acme" with two applications (Website, API), one deployment and
  three members. Logged-in user: owner@acme.test.
seed:
  command: pnpm seed:voyager established-team
  timeout: 2m          # optional
  cwd: .               # optional, relative to the repo root
```

How Voyager runs it:

- **When:** after the app has been started and its health check passes, so a
  seed may call the app's HTTP API. It runs again before every journey that
  uses the scenario. `voyager seed <scenario>` does the same: start the app if
  needed, seed, stop.
- **How:** through a shell (`sh -c`), from the repository root (or `cwd`),
  inheriting Voyager's environment plus `VOYAGER_BASE_URL`, `VOYAGER_SCENARIO`
  (the scenario id) and `VOYAGER_JOURNEY`. The default timeout is 2 minutes.

The seed command **must**:

- **Reset, then insert.** Wipe or truncate everything the journeys can see,
  then create the fixture. Never only insert, because data would accumulate
  between runs.
- **Be deterministic.** Use fixed names, ids, emails and timestamps, with no
  randomness and no `now()` in visible data. The same command must always
  produce the same visible state; screenshots depend on it.
- **Be idempotent and fast.** It runs before every journey, so aim for less
  than a few seconds.
- **Exit non-zero on failure** and print why. Voyager shows the output and keeps
  it in `.voyager-output/_logs/seed-<scenario>.log`.
- **Name its scenario explicitly** in the command (`pnpm seed:voyager
  established-team`) rather than relying only on `VOYAGER_SCENARIO`. That keeps
  the command runnable by hand, and if an existing scenario's command has no
  argument, add one.
- **Reject unknown scenario names** with a non-zero exit, never silently
  seeding nothing.
- Never print secrets.

Prefer a single seed script that takes the scenario name, with one small,
named fixture per scenario. Keep fixtures minimal: only what journeys and
screenshots need. Name scenarios after the product state (`empty`,
`established-team`, `billing-overdue`), not after a journey.

Check the scenario on its own before writing journeys:

```bash
npx voyager seed established-team   # run it
npx voyager seed established-team   # run it again: must succeed and give the same state
```

## Step 3: journeys

```yaml
# .voyager/journeys/applications/create.yaml
id: create-application            # unique, lowercase-kebab
title: Create an application
area: applications                # must exist in product.yaml
tags: [core]
scenario: established-team        # must exist in scenarios/
goal: >
  A workspace member can create a new application.
steps:
  - action: goto
    path: /applications
  - action: wait
    text: Website
  - action: capture
    name: applications
    description: The application list for an established workspace.
    use: { docs: true }
  - action: do
    instruction: Create a new application named "Storefront"
  - action: verify
    instruction: An application named "Storefront" appears in the application list
  - action: capture
    name: application-created
    use: { docs: true, marketing: true }
outputs:                          # defaults: e2e/screenshots/video true, docs/marketing false
  docs: true
  marketing: true
```

### Steps

| action | kind | fields |
| --- | --- | --- |
| `goto` | deterministic | `path` (fails on HTTP ≥ 400) |
| `reload` | deterministic | |
| `wait` | deterministic | exactly one of `text`, `selector`, `url` (glob such as `/apps/*`), `load_state`, `duration`; optional `timeout` (`10s`) |
| `capture` | deterministic | `name` (unique in the journey), `full_page`, `description`, `use: {docs, marketing}` |
| `do` | semantic (AI) | `instruction` |
| `verify` | semantic (AI) | `instruction` |

All steps accept an optional `label` for reports and an optional `narration`:
one or two spoken sentences for narrated demo videos (`voyager demo
--narrate`, ElevenLabs). Write narration for a viewer: describe the benefit,
not the clicks ("Creating a project takes seconds"). Keep each line shorter
than the step it accompanies; Voyager holds the step until the line ends.
Narration never affects test or capture runs.

Durations (`timeout`, `duration`) accept `500ms`, `30s`, `2m`, `1h`, or a
number of milliseconds.

### Outputs and screenshot metadata

- Journey `outputs` decide **which modes run the journey**: `e2e` for `test`,
  `screenshots` for `capture`, `video` for `demo`. `docs` and `marketing` are
  labels kept in the report for downstream tooling.
- A `capture` step's `use` (default: none) and `description` are metadata only.
  They mark which screenshots suit docs or marketing and appear in
  `report.json`/`report.md`, but they never change execution.

### How semantic steps work (write for this)

`do` and `verify` use TypeSafe Jev, a model that **selects** but does not
generate. On each iteration Voyager lists the visible controls (role and
accessible name, plus the surrounding row text) and asks Jev which one to use
next, and which candidate value belongs in each text field. Consequences:

- **Quotes mean "text to type".** Every quoted string becomes a candidate
  value for text fields, and Jev can only type quoted values:
  `Create an application named "Storefront"`. **Name buttons, links and
  other controls without quotes**, using their visible label:
  `…then click Done`, not `…then click "Done"`. Jev matches controls by
  meaning, so quoting labels only adds wrong candidates for typing.
- **Secrets**: `Log in as "owner@acme.test" with password ${env:VOYAGER_PASSWORD}`.
  Jev sees only a placeholder, and the value is redacted everywhere. The variable
  must be set when Voyager runs.
- **Say what "done" looks like when the result is not obvious.** A `do` ends
  when Jev judges the instruction accomplished. If nothing is left to do,
  it also asks whether the actions taken completed the task. For steps whose
  effect is not literally visible, naming the expected result makes both
  judgments reliable: `Sign in as "olivia@acme.test" with password
  ${env:VOYAGER_PASSWORD} until the dashboard shows Olivia Owner`.
- **One user goal per `do`**, phrased like a user would ("Invite
  "bo@acme.test" as a Developer"), not as clicks. Up to 12 interactions are
  allowed per step.
- **Make `verify` concrete and visible**: name the exact text or state that
  should be on the page. While a **modal dialog is open, `verify` (and `do`) see
  only the dialog**, not the page behind it. To check a list behind a dialog,
  close it or `goto` the page first. Avoid vague claims ("it works") and claims that
  need arithmetic or date comparison.
- **Use deterministic steps whenever you know the answer.** Navigate with
  `goto` instead of `do: open the settings page`, and wait for known text with
  `wait`. They are free, instant and never flaky.
- **Waiting:** after every interaction Voyager waits until the page settles
  (no network requests in flight, up to 2s), and `verify` judges the page as it
  is at that moment. If the UI updates later than that (polling, animations,
  delayed renders), put a `wait` with the expected `text` before `verify`.
  It is cheap and deterministic. Otherwise it is optional.
- `do` needs controls with accessible names (labels, button text,
  `aria-label`). If Jev cannot find a control, fix the instruction or note the
  accessibility gap; do not work around it.

### Coverage habits

- One journey = one user goal. Prefer several short journeys to one long one.
- Start with `goto`, end with a `verify` that proves the goal, and `capture`
  the before/after states that docs or marketing would want.
- Put the area in `product.yaml` with code globs, so `voyager update` can
  map changes to journeys.

### Replay

The first time a `do` step succeeds, Voyager records the concrete clicks and
inputs in `.voyager-output/<journey>/trace/trace.json`. A plain `voyager test`
then **replays** them without asking Jev, which is faster and free. If the UI
has changed it falls back to Jev automatically. `verify` always asks Jev.
Recordings live only in the (git-ignored) output directory, so CI starts
fresh. While authoring, always use `--no-replay`, so you test the instruction
rather than an old recording.

## Step 4: run, read, fix

```bash
npx voyager validate
npx voyager test create-application --no-replay --verbose
```

`--verbose` prints every Jev decision. On failure, read in this order:

1. The terminal output: failing step and reason.
2. `.voyager-output/<journey>/failure.png`: what the page actually showed.
3. `.voyager-output/<journey>/report.md`: steps, interactions and assertions with probabilities.
4. `.voyager-output/<journey>/trace/trace.json`: every decision and action.
5. `.voyager-output/<journey>/diagnostics/`: console errors and failed requests.
6. `.voyager-output/_logs/`: app start and seed command output.

Typical fixes:

| Symptom | Fix |
| --- | --- |
| "could not find a suitable action" | Nothing on the page matched before any action was taken. Check `failure.png` and any `Page says: …` message, rephrase with the real labels, or `goto` the right page first. |
| "did not complete the instruction (completion p=…)" | Actions were taken but the result was not judged complete. Read `Page says: …` (e.g. a validation error). If the task really did succeed, name the visible result in the instruction (see above). |
| "contains no value to type" | Quote the value in the instruction. |
| `verify` fails with low probability | The claim is not visible as written. Match the page wording, or the app really is broken. |
| Passes once, fails on the second run | The seed does not reset everything. Fix the seed, not the journey. |
| Seed command failed | Read `_logs/seed-<scenario>.log`. |
| Exit 3, key not set | `TYPESAFE_API_KEY` is required for `do`/`verify`. |

When a journey fails because **the product is broken**, report it. Don't
change the journey to match the bug.

Finally, run the journey a second time (step 3 of the rule), then check
the screenshots in `.voyager-output/<journey>/screenshots/` look right.

## Installing Voyager from a local checkout

If Voyager is installed from a source directory (`npm install -D ../voyager`),
npm may skip its build script, so the package runs whatever is in the
checkout's `dist/`. If `npx voyager` reports "Voyager is not built", or
Voyager's source has changed, rebuild it: `cd <voyager checkout> && pnpm
install && pnpm build`. `npx voyager --version` shows which build is in use.

## Reference

```text
npx voyager init                             # create .voyager/ (refuses to overwrite without --force)
npx voyager validate
npx voyager test|capture|demo [journey...]   # by id, path (area/name) or directory
npx voyager run [journey...] --mode <mode>   # same as test/capture/demo
npx voyager seed <scenario>                  # start the app if needed, run only the seed
npx voyager update --base main               # which journeys a branch's changes affect
npx voyager propose --goal "..." [--apply]   # LLM drafts a journey; kept only if it passes twice
npx voyager skill [install]                  # this document
npx voyager doctor                           # check Node, browser, keys, config, app
Options: --no-replay  --verbose  --no-env (app already running)  --headed
         --output <dir>  --ci (no colors; GitHub annotations and job summary)
Exit codes: 0 pass · 1 journey failed · 2 invalid files/usage · 3 runtime error · 4 not implemented
```

Full documentation: the Voyager README, or `npx voyager --help`.
