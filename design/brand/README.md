# Brand

The colours of shpyrd, and how the logo sits on each background.

## Colours

| Name | Hex |
|---|---|
| Orange | `#ff4f00` |
| Black | `#000000` |
| White | `#ffffff` |

In the interface, text and icons on the orange are always white.

## The logo on each background

| Background | Symbol | Name | File |
|---|---|---|---|
| Dark | orange | white | `logo/logo-full-dark` |
| Orange | white | black | `logo/logo-full-on-orange` |
| White | orange | black | `logo/logo-full` |

The symbol alone follows the same rule: orange on dark or white
(`logo-symbol`), white on orange (`logo-symbol-white`). The app icon is the
white symbol on an orange square (`logo-symbol-rect`), or the orange symbol on
a black one (`logo-symbol-rect-black`).

When colour cannot be used, the logo is all black (`logo-full-black`) or all
white (`logo-full-white`).

## Files

Every variant is in `logo/` as SVG, PNG and PNG `@2x`. Symbols are 512 px wide
(1024 at `@2x`); the full logo is 512 px tall (1024 at `@2x`).

## In the code

`LogoMark` and `Wordmark` in `design/ui` (`src/components/brand.tsx`) follow
the same rule through `surface`: `"dark"`, `"orange"`, `"light"`, or `"auto"`
(the default, light or dark by the theme).

```tsx
<Wordmark surface="orange" />
```
