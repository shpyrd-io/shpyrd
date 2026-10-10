// /getting-started — the journey, in the order the research's demonstration
// script uses: the useful work first, the platform second.
//
// Each step names the screenshot it needs. Two of them do not exist yet; see
// the spec's dependency note.

export const intro = {
  title: 'Getting Started',
  lead:
    'Four steps, in the order they happen. An app is published, a colleague ' +
    'opens it and does real work, someone outside the team is turned away, and ' +
    'the person who maintains it ships a change and takes it back.',
}

export const steps = [
  {
    id: 'publish',
    step: 'publish',
    title: 'The app is published',
    body:
      'Its maintainer deploys from the code they already have. The cluster ' +
      'builds it and gives it a URL with TLS, and sign-in sits in front of it ' +
      'from the first release.',
    screenshot: {
      src: '/screenshots/deploy-dialog-crop.png',
      alt: 'Deploying a project from the dashboard',
      exists: true,
    },
  },
  {
    id: 'open',
    step: 'access',
    title: 'A colleague opens it and does the work',
    body:
      'Someone in Finance signs in and sees the apps they are allowed to open — ' +
      'not a list of everything that exists. They open the tracker and complete ' +
      'a real task. This is the part that matters; everything else is in ' +
      'service of it.',
    screenshot: {
      src: '/screenshots/launcher.png',
      alt: 'The app launcher as a use-only colleague sees it',
      exists: true,
    },
  },
  {
    id: 'denied',
    step: 'access',
    title: 'Someone outside the team is turned away',
    body:
      'An account without access to that app does not reach it. The check ' +
      'happens before the request gets to your application, so it does not ' +
      'depend on your app implementing sign-in correctly.',
    screenshot: {
      src: '/screenshots/denied.png',
      alt: 'An account without access being denied',
      exists: true,
    },
  },
  {
    id: 'operate',
    step: 'operate',
    title: 'The maintainer ships a change, and takes it back',
    body:
      'Every deploy and config change is a numbered release. When a change ' +
      'turns out to be wrong, rolling back restores the previous release and ' +
      'the config that went with it.',
    screenshot: {
      src: '/screenshots/releases-card-crop.png',
      alt: 'Numbered releases with a rollback action',
      exists: true,
    },
  },
]

export const runs = {
  step: 'runs',
  title: 'Where all of this runs',
  body:
    'On shpyrd cloud, with nothing to run. Or on a Kubernetes cluster your ' +
    'company controls: locally on kind while you try it, on Oracle Cloud (OKE) ' +
    'or AWS (EKS) when it matters. A cluster of your own needs an owner.',
  // The way to start: the sign-up (apps/website/src/lib/signup.ts).
  action: 'Get started',
}
