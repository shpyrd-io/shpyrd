import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ItNextStep } from "@/components/for-it";
import { Faq } from "@/components/faq";
import { pageSections } from "@/lib/page";

// For IT teams · common questions. The answers, short and straight, to what
// someone asks before letting an app built with AI run for the company,
// answered for shpyrd cloud first; running it in your own cloud is one of the
// questions. Each answer points at the doc that proves it.

export const metadata = { title: "For IT teams · Common questions" };

const answers = [
  {
    q: "Where does it run?",
    a: "On shpyrd cloud: your workspace has its own address, and there is nothing to install or operate. Apps get their own addresses under it, and can have your own domains.",
    link: { label: "Domains", href: "/docs/domains" },
  },
  {
    q: "Can it run in our own cloud instead?",
    a: "Yes, when policy needs it: the same platform installs on AWS (EKS) or Oracle Cloud (OKE), with Terraform for the infrastructure. On your own cluster, apps can also be kept internal, reachable only from your network. Someone then has to own that cluster.",
    link: { label: "Installation", href: "/docs/installation" },
  },
  {
    q: "Who can open an app?",
    a: "Only people given a role on it, after signing in with your company's account. Everyone else gets a page saying which team it is for. The check happens before the request reaches the app.",
    link: { label: "Sign-in for your app", href: "/docs/app-access" },
  },
  {
    q: "Who can change it?",
    a: "Whoever you give the developer or admin role. Using an app and changing it are separate rights, per app, to a person or a team.",
    link: { label: "People, teams and roles", href: "/docs/access" },
  },
  {
    q: "What happens when someone leaves?",
    a: "Suspend them and every app closes to them at once. Removing them from your identity provider does the same when their session ends.",
    link: { label: "Suspending someone", href: "/docs/access" },
  },
  {
    q: "What if an update breaks it?",
    a: "Every deploy is a numbered release; rolling back restores the code and its settings. It does not reverse database migrations.",
    link: { label: "Deploying", href: "/docs/deploying" },
  },
  {
    q: "What is backed up?",
    a: "Every night, the workspace itself: its projects, settings, teams and who can open what, encrypted. A database can have its own continuous backups, restorable to any second in their window. Files an app keeps on a volume are not in either.",
    link: { label: "Databases and backups", href: "/docs/databases" },
  },
  {
    q: "What is recorded?",
    a: "Every change to the platform: who deployed, rolled back, changed a setting or a member. Today that record is short-lived (an hour by default), and it is not a record of what people did inside an app.",
    link: { label: "Audit trail", href: "/docs/access" },
  },
  {
    q: "Does the app still need its own permissions?",
    a: "For anything finer than who may open it, yes. shpyrd tells the app who is there and which teams they are in; “can approve over €5,000” is the app's job.",
    link: { label: "What the app receives", href: "/docs/app-access" },
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero align="center"
        variant="page"
        heading="Someone built an app with AI. Here's what you'll want to know."
        description="Straight answers for shpyrd cloud, including the ones where the answer is “not yet”."
      />

      <Faq items={answers} />

      <ItNextStep next={{ label: "Before you approve", href: "/for/it/before-you-approve" }} />
    </PageLayoutContent>
  );
}
