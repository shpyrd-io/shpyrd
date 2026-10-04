# License keys

The public keys licenses are verified against, one file per key:
`<kid>.pub`, PEM (SPKI), as `shpyrd-license keygen` writes it. The file name
is the `kid` a license's header names.

A key is added, never replaced: licenses already issued keep naming theirs.
`2026-10.pub` is the first production key. The private keys live outside
every public repository: in the license tool's repository, out of git.
