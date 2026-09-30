import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { PartnerStep } from "@/components/for-fde-partners";

// For FDE partners - handing it over: what the client holds when you leave, on
// shpyrd cloud first (docs: domains, access, deploying), and taking your team
// off in one line. Backups belong to whoever runs the cluster, so they are on
// the client's-cloud tab, not here.

export const metadata = { title: "For FDE partners · Handing it over" };

const keeps = [
  { what: "The workspace", how: "Theirs, on shpyrd cloud. Nothing of it lives with you." },
  { what: "Its address", how: "On the client's own domain when they bring one, with its certificates." },
  { what: "Who gets in", how: "The client's sign-in and groups. Their IT adds and removes people." },
  { what: "Every change", how: "Numbered releases with the config that went with each, and a way back to any of them." },
  { what: "The platform itself", how: "Open source, MPL-2.0. They can read every line of it." },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        variant="medium"
        heading="Hand over the app, not a pile of setup."
        description="Every client asks the same question at the end: who runs this now? The answer is already there: their workspace, behind their sign-in."
      />

      <Stack gap="spacious">
        <SectionIntro heading="What the client holds when you leave" description="Not a document describing the setup. The setup." />
        <Card className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-1/3" />
                <TableHead>What they have</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {keeps.map((k) => (
                <TableRow key={k.what}>
                  <TableCell className="align-top font-medium whitespace-normal">{k.what}</TableCell>
                  <TableCell className="align-top whitespace-normal text-muted-foreground">{k.how}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro
          heading="When the work ends, you leave in one line"
          description="Your team was given access as a team, so taking it away is one change, not an afternoon of hunting for keys."
        />
        <Card className="max-w-2xl p-0">
          <IDE
            language="sh"
            showLineNumbers={false}
            code={`# your team, while you deliver
shpyrd members add orders --team partner-engineers --role developer

# the client's staff, for good
shpyrd members add orders --team warehouse --role user

# the day you hand over
shpyrd members remove orders --team partner-engineers`}
          />
        </Card>
      </Stack>

      <PartnerStep next={{ href: "/for/fde-partners/in-the-clients-cloud", title: "In the client's cloud" }} />
    </PageLayoutContent>
  );
}
