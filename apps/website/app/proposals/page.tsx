import Link from "next/link";
import { Card, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { proposals, sharingProposals } from "@/components/proposals";

// The four homepage proposals, side by side, and what each one is testing.

export const metadata = { title: "Homepage proposals" };

const about: Record<number, { headline: string; tests: string }> = {
  1: {
    headline: "You built it. Now let your team use it.",
    tests: "H1, sharing. Starts on the builder's laptop and walks to the colleague.",
  },
  2: {
    headline: "Build it with your agent. Share it the same way.",
    tests: "H4, familiar tools. The whole page is one more turn of the conversation.",
  },
  3: {
    headline: "Your team's apps, in one place.",
    tests: "H2 and H5, the workspace and its roles, shown as each person sees it.",
  },
  4: {
    headline: "Sharing an app shouldn't mean sharing your laptop.",
    tests: "H3, access. Names today's setup (a link and a password) and answers it.",
  },
  5: {
    headline: "You built it. We ship it.",
    tests: "Today's homepage with a chat in place of the two windows: build, ship, share, in the agent's own words.",
  },
};

const aboutSharing: Record<number, { headline: string; tests: string }> = {
  1: {
    headline: "Tell your agent who it's for.",
    tests: "One sentence, one answer, and four things you might say later.",
  },
  2: {
    headline: "Say it the way you'd say it.",
    tests: "Six things people say to their agent, and what it answers.",
  },
  3: {
    headline: "You say it. They're in.",
    tests: "The sentence on one side; what Ana, and Pedro, see on the other.",
  },
  4: {
    headline: "Who should have it?",
    tests: "Tap names; the sentence to the agent and its answer write themselves.",
  },
};

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-8 py-8">
      <PageHeading
        title="Homepage proposals"
        description="Four ways to open the site for people who can build with AI and don't know how to put what they built in front of others."
      />
      <ul className="grid gap-4 sm:grid-cols-2">
        {proposals.map((p) => (
          <li key={p.n}>
            <Card asChild className="h-full">
              <Link href={p.href}>
                <CardHeader>
                  <CardDescription>
                    {p.n} · {p.title}
                  </CardDescription>
                  <CardTitle className="font-heading text-lg">{about[p.n].headline}</CardTitle>
                  <CardDescription>{about[p.n].tests}</CardDescription>
                </CardHeader>
              </Link>
            </Card>
          </li>
        ))}
      </ul>

      <PageHeading
        as="h2"
        title="How sharing works proposals"
        description="Sharing is one sentence to your agent. Four ways to show that."
      />
      <ul className="grid gap-4 sm:grid-cols-2">
        {sharingProposals.map((p) => (
          <li key={p.n}>
            <Card asChild className="h-full">
              <Link href={p.href}>
                <CardHeader>
                  <CardDescription>
                    {p.n} · {p.title}
                  </CardDescription>
                  <CardTitle className="font-heading text-lg">{aboutSharing[p.n].headline}</CardTitle>
                  <CardDescription>{aboutSharing[p.n].tests}</CardDescription>
                </CardHeader>
              </Link>
            </Card>
          </li>
        ))}
      </ul>
    </PageLayoutContent>
  );
}
