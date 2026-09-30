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
export const contact = {
  href: 'https://discord.gg/AxWMXXW7',
  label: 'Start the conversation',
  note: 'Conversations happen in the project Discord while shpyrd is in beta.',
}

export const secondaryCta = {
  label: 'See how sharing works',
  href: '/how-sharing-works',
}

export const developerCta = {
  label: 'Run it yourself',
  href: '/docs/getting-started',
}
