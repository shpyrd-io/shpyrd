# @shpyrd/ui

The design system of shpyrd: its components, its tokens and its styles, as
React source for Next applications. The gallery that shows every component
is this folder's Next application (`npm run dev --workspace design/ui`).

## In an application of another repository

```sh
npm install @shpyrd/ui
```

The package is TypeScript source, and Tailwind reads its classes from it, so
an application needs two things:

```ts
// next.config.ts
const config = { transpilePackages: ["@shpyrd/ui"] };
export default config;
```

```css
/* the application's global stylesheet */
@import "@shpyrd/ui/styles.css";
```

Then: `import { Button } from "@shpyrd/ui/components/button";`.

It needs React 19, Next 16 and Tailwind 4 in the application
(`peerDependencies`).

## In this repository

The applications of this repository use the library through the npm
workspace (`"@shpyrd/ui": "*"`): they always see it as it is in the
working tree. How the library is worked on is in
`design/DESIGN_FOR_AGENT.md`.

## Versions

The version is the one in this folder's `package.json`. Before 1.0, a
change that breaks a component (a prop renamed or removed, a component
removed, a behaviour a screen relies on changed) is a minor version
(`0.2.0` to `0.3.0`); anything else, a patch. Applications depend on
`^0.x.y`, which takes patches only.

`0.0.0` is the version of a library never released.

## Releasing

1. In the pull request that changes the library, raise the version in
   `package.json` and add its section to `CHANGELOG.md`.
2. Merge it. On `main`, `.github/workflows/ui-release.yml` sees a version
   npm does not have, runs the library's lint, type-check and tests,
   publishes it, tags the commit `ui-v<version>` and makes a GitHub release
   with the version's section.

A version with a suffix (`0.4.0-rc.1`) is published under the `next`
dist-tag, never `latest`.
