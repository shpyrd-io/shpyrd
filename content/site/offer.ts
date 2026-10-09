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

// The site's main action: what someone built, deployed on shpyrd cloud
// (the sign-up). The words take turns after "Deploy your".
export const deploy = {
  label: 'Deploy your',
  things: ['vibecoded app', 'agent', 'site', 'API', 'internal tool', 'side project'],
}

// Where "Contact us" leads: the sales form, and the enterprise form for the
// Enterprise plan (apps/website/app/contact).
export const contact = {
  href: '/contact/sales',
  label: 'Contact us',
}

export const enterpriseContact = {
  href: '/contact/enterprise',
  label: 'Contact us',
}

export const secondaryCta = {
  label: 'Getting Started',
  href: '/getting-started',
}

export const developerCta = {
  label: 'Run it yourself',
  href: '/docs/getting-started',
}
