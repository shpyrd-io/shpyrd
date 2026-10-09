// The self sign-up of shpyrd cloud, with a plan chosen: the sign-up reads
// `?plan=` (the plans' ids, content/site/pricing.ts). "Get started", and
// anything that names no plan, chooses the free one.
export const signUpFor = (plan = "free") => `https://signup.shpyrd.io/?plan=${encodeURIComponent(plan)}`;
export const signUp = signUpFor();
