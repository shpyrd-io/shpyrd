// The assisted beta offer, and the calls to action that lead to it.
//
// There is no self-service sharing journey today, so the primary action goes to
// a conversation. "Share your first app" is the wording for when that journey
// exists; until then it promises something we cannot deliver.

// Where "start the conversation" actually goes.
//
// TODO: this is the weakest link in the offer. Discord is real and verified,
// but it is an odd front door for an operations buyer, and the research is
// clear that the destination has to match the promise. Replace it with a
// monitored address or a booking link before this page is promoted anywhere.
export const contact = {
  href: 'https://discord.gg/AxWMXXW7',
  label: 'Start the conversation',
  note: 'Conversations happen in the project Discord while shpyrd is in beta.',
}

export const primaryCta = {
  label: 'Bring an app to a sharing session',
  href: '/bring-an-app',
}

export const secondaryCta = {
  label: 'See how sharing works',
  href: '/how-sharing-works',
}

export const developerCta = {
  label: 'Run it yourself',
  href: '/docs/getting-started',
}

export const offer = {
  title: 'Bring an app to a sharing session',
  intro:
    'shpyrd is in beta, and the way in is a conversation rather than a signup ' +
    'form. You bring an app your team has already built. Together we work out ' +
    'whether it can reach the colleagues waiting for it, and what that would take.',

  bring: {
    title: 'What to bring',
    items: [
      {
        title: 'A working app',
        body:
          'Something your team built and uses — a tracker, a dashboard, an ' +
          'internal tool. It does not have to be finished. It does have to run.',
      },
      {
        title: 'The person who maintains it',
        body:
          'Whoever deploys it and fixes it when it breaks. Most of the useful ' +
          'questions are theirs to answer.',
      },
      {
        title: 'The colleague who is waiting for it',
        body:
          'The person who wants to use the app and currently cannot. They are ' +
          'the reason to do any of this, and the test of whether it worked.',
      },
    ],
  },

  happens: {
    title: 'What happens',
    items: [
      'We look at the app together: framework, runtime, data, and the external services it depends on.',
      'We work out who should be able to open it, and who should be able to update or administer it.',
      'We agree where it would run and who would own that cluster.',
      'You get a straight answer about fit, including when the answer is no.',
    ],
  },

  scope: {
    title: 'What this is not',
    items: [
      {
        title: 'Not a managed service',
        body:
          'There is no SLA, no managed operations offering and no support ' +
          'commitment today. If we build those, they will be described ' +
          'separately and honestly.',
      },
      {
        title: 'Not application development',
        body:
          'We do not build, debug or rewrite your app. Its business logic, its ' +
          'data model and its own permissions stay yours.',
      },
      {
        title: 'Not a migration service',
        body:
          'Moving an app that already works somewhere else needs a reason that ' +
          'outweighs the cost. Often there is not one, and we will say so.',
      },
    ],
  },

  poorFit: {
    title: 'When this is a bad fit',
    intro: 'Worth saying plainly, because it saves both of us a call:',
    items: [
      'Nobody at your company can own a Kubernetes cluster, and you have no implementation partner. A self-managed install is a poor offer for you.',
      'Your current host or app builder already handles your hosting, access, ownership and support requirements. Staying there is the better choice.',
      'What you actually need is someone to build the app. That is a different engagement, and not one we offer.',
      'You need a compliance-grade audit trail of what users did inside an app. Read the boundaries below before we talk.',
    ],
  },
}
