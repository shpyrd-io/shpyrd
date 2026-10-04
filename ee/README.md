# ee

The enterprise features of shpyrd. The source is public, like the rest of
the repository, but it is not MPL-2.0: [`LICENSE`](LICENSE) here says it may
run in production only under an agreement and with a valid license.

- **A license** switches every feature in this folder on until the day it
  expires; it carries no limits. The operator installs it with
  `shpyrd-ctl license set <file>`; the console shows its status
  ([`licensing/`](licensing)).
- **Without one**, the code is built in but does nothing: its routes answer
  402 "available with a license" (`licensing.Require()`), its loops stay idle.
- **The foss build** has none of it: every Go file here starts with
  `//go:build !foss`, and `go build -tags foss ./...` gives binaries that are
  MPL-2.0 throughout. CI builds it so the core never comes to depend on this
  folder.
- **The extensions here** are listed in [`all/`](all) and are always on
  where they are built in; the license decides what they do.

Changes to this folder are made by the shpyrd team for now.
