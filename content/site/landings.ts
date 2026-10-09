// The shape of a feature or solution page (content/site/features.ts and
// content/site/solutions.ts), after the page construction in the copy
// guidelines (arquivos de copy, 2026-10-09): the outcome, an example, the
// capabilities behind it, the practical questions, one next step.
//
// An icon is named, not drawn: the website draws it (lucide). A link's
// `href` may be 'signup' (a workspace on shpyrd cloud) or 'contact' (the
// conversation), which the website resolves.

export type Icon =
  | 'rocket' | 'cloud' | 'database' | 'lock' | 'moon' | 'activity'
  | 'git' | 'terminal' | 'globe' | 'history' | 'shield' | 'archive'
  | 'server' | 'cpu' | 'hard-drive' | 'users' | 'key' | 'layout'
  | 'bot' | 'sparkles' | 'link' | 'pencil' | 'briefcase' | 'building'
  | 'handshake' | 'tag' | 'layers' | 'scroll' | 'gauge' | 'code'
  | 'file' | 'wrench' | 'refresh' | 'eye' | 'coins' | 'package'

export type Link = { label: string; href: string }
export type Item = { icon: Icon; heading: string; body: string }

export type Landing = {
  slug: string
  // In the menu: its name, a line under it, its icon.
  name: string
  card: string
  icon: Icon
  // The page.
  label: string
  heading: string
  description: string
  primary: Link
  secondary?: Link
  example: { heading: string; description?: string; steps: [Item, Item, Item] }
  capabilities: { heading: string; description?: string; items: Item[] }
  questions: { heading: string; items: { q: string; a: string }[] }
  related: Link[]
  close: { heading: string; description: string; action: Link }
}
