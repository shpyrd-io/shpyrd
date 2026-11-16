# Documentation

This folder is the repository's knowledge base, following the shpyrd-harness
brain convention (version in `../.shpyrd/harness.yaml`).

- `decisions/`: decisions and why they were made.
- `specs/`: how something works or should work.
- `lessons/`: what incidents, mistakes and escaped defects taught us.
- `glossary.md`: terms with a specific meaning in this repository.

Every document starts with frontmatter (`id`, `type`, `title`, `status`,
`date`, `related`, `governs`) and is referenced by its ID. Documentation
changes in the same pull request as the code it describes. CI checks the
frontmatter, IDs and references.
