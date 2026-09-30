// The message families the positioning research asks us to compare.
//
// The first comparison holds audience, offer, demo and CTA constant and varies
// only the explanation, so those live in `offer.ts` and are shared by all three
// families here. Test CTA wording separately, once it is known which
// explanation people understand correctly.
//
// See docs/superpowers/specs/2026-09-29-website-positioning-design.md.

export type Message = {
  id: string;
  headline: string;
  explanation: string;
  supporting: string | null;
};

export const sharing: Message = {
  id: 'sharing',
  headline: 'One place to share apps with your team.',
  explanation:
    'Bring the apps your team builds, choose who can use or manage them, and ' +
    'give colleagues one place to find them.',
  supporting:
    'Connect the agent you already use. It deploys your apps, watches their ' +
    'logs and metrics, and shares them with the colleagues who need them.',
}

export const access: Message = {
  id: 'access',
  headline: 'Give each team access to the apps it needs.',
  explanation:
    'Manage who can use, update and administer the apps in your workspace.',
  supporting:
    'Connect the agent you already use. It deploys your apps, watches their ' +
    'logs and metrics, and shares them with the colleagues who need them.',
}

export const familiarTools: Message = {
  id: 'familiar-tools',
  headline: 'Build with Claude Code or Codex. Share with your team through shpyrd.',
  explanation:
    "Bring your team's apps into a shared workspace for the colleagues who need them.",
  supporting: null,
}

// The family the site currently ships. Swapping it is a one-line change.
export const active = sharing
