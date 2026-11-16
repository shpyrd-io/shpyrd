# AGENTS.md

<!-- shpyrd-harness:begin -->
## shpyrd-harness

This repository uses shpyrd-harness; the version, the repo prefix and the
`verify` commands are in `.shpyrd/harness.yaml`.

Work:

- A change to code follows the harness development cycle: use the `run-task`
  skill. Acceptance criteria are written before the code, and a separate
  agent judges them.
- Run verify before calling anything done. Never weaken or delete a check or
  a test to make it pass.
- `.shpyrd/runs/` holds working records and is never committed.

Documentation:

- It lives in `docs/`: `decisions/`, `specs/`, `lessons/` and `glossary.md`.
  Every document has an ID such as `<prefix>-dec-012`; refer to documents
  only by ID, also across repositories.
- Before changing code, read the documents whose `governs` lists it.
- Update the documentation in the same pull request as the code it describes.
- What is true for this repository alone stays here; what spans repositories
  goes in the workspace. Never copy content from another repository.
- This repository is public: never mention private repositories, internal
  services, infrastructure, customers or future plans in it, and never
  describe a security weakness that is not yet fixed.
<!-- shpyrd-harness:end -->
