// What shpyrd does not do.
//
// One source, rendered on the homepage and again, in fuller form, alongside the
// matching step of /getting-started. The rule that goes with this file: a
// capability claim on a marketing page maps to shipped code, not to an RFC
// title.

export type Boundary = {
  id: string;
  claim: string;
  limit: string;
  step: string | null;
};

export const boundaries: Boundary[] = [
  {
    id: 'sign-in',
    claim: 'Sign-in controls who can open an app.',
    limit:
      "It doesn't control what they can do inside it. “Can open the expense " +
      'app” and “can approve expenses above $5,000” are different ' +
      "requirements — the second one is your app's job.",
    step: 'access',
  },
  {
    id: 'compatibility',
    claim: '“Compatible” needs checking.',
    limit:
      'Framework, runtime, data and external services all matter. Working ' +
      'code can still depend on services that need separate configuration.',
    step: 'publish',
  },
  {
    id: 'rollback',
    claim: 'Rolling back restores the release and its config.',
    limit:
      "It doesn't reverse database migrations or undo external side effects.",
    step: 'operate',
  },
  {
    id: 'data',
    claim:
      "Running on shpyrd cloud, or in your own cluster, isn't the same as your data never leaving.",
    limit: 'An app that calls an external API still calls it.',
    step: 'runs',
  },
  {
    id: 'audit',
    claim: "Platform activity is recorded, but it isn't a compliance audit trail.",
    limit:
      "Retention currently follows the cluster's event retention. And platform " +
      'activity is a different thing from an audit of what people did inside ' +
      'an app.',
    step: 'operate',
  },
  {
    id: 'licence',
    claim: 'Open source, MPL-2.0.',
    limit:
      'You can read it, run it, and plan to operate it yourself. That isn’t ' +
      'the same as zero migration work.',
    step: null,
  },
]

export function boundariesForStep(step: string): Boundary[] {
  return boundaries.filter((boundary) => boundary.step === step);
}
