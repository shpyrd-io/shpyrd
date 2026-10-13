import { copy } from "@shpyrd/content/site/shared/boundaries.mdx";
import type { BoundariesCopy } from "./contracts";

export const { boundaries } = copy as BoundariesCopy;
export type Boundary = (typeof boundaries)[number];
export function boundariesForStep(step: string): Boundary[] { return boundaries.filter((item) => item.step === step); }
