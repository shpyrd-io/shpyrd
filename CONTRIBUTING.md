# Contributing

shpyrd is [MPL 2.0 licensed](LICENSE) and accepts contributions via GitHub
pull requests. This document outlines some of the conventions to make it
easier to get your contribution accepted.

One directory is licensed differently: the website in [`website/`](website) is
built on the commercial Tailwind UI *Syntax* template and is governed by
[`website/LICENSE`](website/LICENSE). Content contributions are welcome on those
terms — Markdown under `website/src/pages/` is ordinary documentation. Changes to
the template itself, meaning the components and styles under
`website/src/components/` and `website/src/styles/`, need your own Tailwind UI
licence, because a derivative of the template stays under the template's terms.

Design changes go through the [RFC process](rfcs/README.md); see
[RFC-0001](rfcs/0001-mvp-local-platform.md) for the current architecture.

We gratefully welcome improvements to issues and documentation as well as to
code.

## Certificate of Origin

By contributing to this project you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution.

We require all commits to be signed. By signing off with your signature, you
certify that you wrote the patch or otherwise have the right to contribute the
material by the rules of the [DCO](https://developercertificate.org/):

`Signed-off-by: Jane Doe <jane.doe@example.com>`

The signature must contain your real name
(sorry, no pseudonyms or anonymous contributions)
If your `user.name` and `user.email` are configured in your Git config,
you can sign your commit automatically with `git commit -s`.

## TODO

Write issues and community guidelines