import { Eye, Hammer, KeyRound } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { boundariesSection } from "@shpyrd/content/site/home";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Boundaries } from "@/components/boundaries";
import { Closing, ProposalNav } from "@/components/proposals";

// Proposal 4 - today vs. shpyrd. Hypothesis H3: managing who gets in is the
// job worth paying for. Its rival is the research's hardest competitor, "keep
// the current setup": the link in a chat, the shared password, the laptop that
// has to stay awake. The page names that setup and answers it line by line.

export const metadata = { title: "Proposal 4 · Today vs. shpyrd" };

const rows = [
  {
    question: "Where does it run?",
    today: "On your laptop, which has to stay awake. Or a free host on your own card.",
    shpyrd: "On shpyrd cloud, or a cluster your company controls, at an address of its own.",
  },
  {
    question: "Who can open it?",
    today: "Anyone who has the link, or the password you pasted in the chat.",
    shpyrd: "The teams and people you chose, after they sign in with their company account.",
  },
  {
    question: "Who can change it?",
    today: "You. Only you.",
    shpyrd: "Whoever you gave update rights to, separately from who can use it.",
  },
  {
    question: "An update breaks it?",
    today: "You fix it forward, tonight, while Finance waits.",
    shpyrd: "Go back to the previous release in one step, then fix it tomorrow.",
  },
  {
    question: "Where do colleagues find it?",
    today: "In a message from three weeks ago.",
    shpyrd: "Among their apps, the next time they sign in.",
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <ProposalNav current={4} />

      <Hero
        heading="Sharing an app shouldn't mean sharing your laptop."
        description="The tool you built with AI works. Today it reaches your team as a link in a chat and a password to go with it. shpyrd gives it a proper home: its own address, your company's sign-in, and a list of who is allowed in."
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
      />

      <Stack gap="spacious">
        <SectionIntro heading="How your app is shared today, and with shpyrd" />
        <Card className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-1/5" />
                <TableHead className="w-2/5">Today</TableHead>
                <TableHead className="w-2/5">With shpyrd</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.question}>
                  <TableCell className="align-top font-medium whitespace-normal">{row.question}</TableCell>
                  <TableCell className="align-top whitespace-normal text-muted-foreground">
                    {row.today}
                  </TableCell>
                  <TableCell className="align-top whitespace-normal">{row.shpyrd}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro
          heading="Three kinds of access, kept apart"
          description="The person who uses an app and the person who can break it are rarely the same person."
        />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar
            icon={<Eye />}
            heading="Use"
            description="Open the app and do the work. Most of the company is here, and nothing they do changes the app itself."
          />
          <Pillar
            icon={<Hammer />}
            heading="Update"
            description="Deploy a new release, change its settings, roll it back. Whoever built it, and whoever covers for them."
          />
          <Pillar
            icon={<KeyRound />}
            heading="Manage"
            description="Decide who is in which team and which apps each team gets. One or two people, for the whole workspace."
          />
        </div>
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading={boundariesSection.title} description={boundariesSection.intro} />
        <Boundaries items={boundaries} />
      </Stack>

      <Closing />
    </PageLayoutContent>
  );
}
