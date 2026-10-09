// The contact pages (apps/website/app/contact): what each asks, and what it
// says beside the form. The rules the page and the server hold the answers
// to are derived from these fields (apps/website/src/lib/contact.ts).
import { pricing } from './pricing'

export type Choice = { value: string; label: string }

export type ContactField = {
  name: string
  label: string
  type: 'text' | 'email' | 'select' | 'textarea'
  required?: boolean
  choices?: Choice[]
  autoComplete?: string
}

export type ContactPage = {
  title: string
  description: string
  submit: string
  fields: ContactField[]
  beside: { heading: string; items: string[] }
  after: { heading: string; items: string[] }
}

const person: ContactField[] = [
  { name: 'firstName', label: 'First name', type: 'text', required: true, autoComplete: 'given-name' },
  { name: 'lastName', label: 'Last name', type: 'text', required: true, autoComplete: 'family-name' },
  { name: 'email', label: 'Company email', type: 'email', required: true, autoComplete: 'email' },
]

const company: ContactField = { name: 'company', label: 'Company', type: 'text', required: true, autoComplete: 'organization' }

const replyFirst = 'A reply within one business day'

export const sales: ContactPage = {
  title: 'Contact sales',
  description: "Tell us about the apps your team would bring, and we'll set up a call.",
  submit: 'Request a call',
  fields: [
    ...person,
    { name: 'message', label: 'How can we help you?', type: 'textarea', required: true },
  ],
  beside: {
    heading: 'What the call covers',
    items: [
      'The apps your team would bring',
      'Who should reach each of them',
      'Where they would run: shpyrd cloud, or your own cloud',
    ],
  },
  after: {
    heading: 'What follows',
    items: [replyFirst, 'A trial workspace on shpyrd cloud, or a plan for your own cloud'],
  },
}

export const enterprise: ContactPage = {
  title: 'Contact enterprise sales',
  description: 'shpyrd in your own cloud, on your terms, with everything shpyrd cloud has. Tell us what your company needs.',
  submit: 'Contact enterprise sales',
  fields: [
    ...person,
    company,
    { name: 'jobTitle', label: 'Job title', type: 'text', required: true, autoComplete: 'organization-title' },
    {
      name: 'size',
      label: 'Company size',
      type: 'select',
      required: true,
      choices: [
        { value: '1-49', label: '1–49' },
        { value: '50-249', label: '50–249' },
        { value: '250-999', label: '250–999' },
        { value: '1000+', label: '1000+' },
      ],
    },
    { name: 'message', label: 'How can we help you?', type: 'textarea', required: true },
  ],
  beside: { heading: 'What Enterprise adds', items: pricing.enterprise.features },
  after: { heading: 'What follows', items: [replyFirst, 'A call about your setup', 'A license for a trial'] },
}

export const privacy = {
  label: 'Privacy Policy',
  href: 'https://legal.shpyrd.io/global/privacy-policy',
}

export const technical = { text: 'Technical question?', link: 'Ask in Discord' }

export const failed = 'The form could not be sent. Try again in a moment, or write to us in Discord.'

export const thanks = (email: string) => `Thanks, we'll reply to ${email} within one business day.`
