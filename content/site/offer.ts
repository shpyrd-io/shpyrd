// The calls to action the pages share, and where they go.
//
// There is no self-service journey and no page describing an offer today: a
// prospect who wants to talk goes to a conversation.

// Where "start the conversation" actually goes.
//
// TODO: Discord is real and verified, but it is an odd front door for an
// operations buyer, and the research is clear that the destination has to
// match the promise. Replace it with a monitored address or a booking link
// before this page is promoted anywhere.
// The project's Discord. The site links to its own /discord, a redirect to the
// invite (apps/website/vercel.json), so the invite can change in one place
// without touching a page; a test keeps the two the same.
export const discord = {
  invite: 'https://discord.gg/RYAT4wNKfw',
  href: '/discord',
}

export const contact = {
  href: discord.href,
  label: 'Start the conversation',
  note: 'Conversations happen in the project Discord while shpyrd is in beta.',
}

export const secondaryCta = {
  label: 'Getting Started',
  href: '/getting-started',
}

export const developerCta = {
  label: 'Run it yourself',
  href: '/docs/getting-started',
}
