// The types of an imported image (the shipyard's pieces are .svg files).
// Next writes them into next-env.d.ts when it runs, but that file is not
// committed, so a typecheck on a fresh checkout needs them from here.
/// <reference types="next/image-types/global" />
