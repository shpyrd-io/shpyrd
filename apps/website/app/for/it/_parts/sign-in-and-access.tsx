import { Building2, FileCode, Mail, UserX, Users } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { CodeWindow } from "@/components/code-window";
import { infoTable } from "@/lib/info-table";
import { pageSections } from "@/lib/page";

// For IT teams · sign-in and access. Who gets into the workspace and into each
// app, and how: your company's sign-in, your groups, the roles in plain words,
// invitations, suspension, and the check at the door. From docs/access and
// docs/app-access.

const roles = [
  { plain: "can look", role: "reader", detail: "Opens the app; anything that would change something is refused at the door." },
  { plain: "can use it", role: "user", detail: "Opens the app and works in it. Sees nothing of how it runs." },
  { plain: "can see how it runs", role: "viewer", detail: "Releases, logs, metrics and the names of its settings." },
  { plain: "can change it", role: "developer", detail: "Deploys, rolls back, changes settings." },
  { plain: "decides who's in", role: "admin", detail: "All of the above, and who holds which of these." },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero as="h2" align="center"
        variant="page"
        heading="Your sign-in, your groups, your rules."
        description="Who gets into the workspace, and into each app in it. You set it up once; builders share inside the rules you set."
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="Getting in" />
        <div className="grid gap-8 sm:grid-cols-2">
          <Pillar variant="card"
            icon={<Building2 />}
            heading="Your company's sign-in"
            description="Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider (Okta, Keycloak, Auth0…). Claim your email domain and people are sent straight to it."
          />
          <Pillar variant="card"
            icon={<Users />}
            heading="Groups become teams"
            description="A group in your directory maps to a team, so nobody keeps two lists. The everyone team holds every person who has signed in, for the apps the whole company should have."
          />
          <Pillar variant="card"
            icon={<Mail />}
            heading="Invitations"
            description="Invite someone by their email address, with a role and a team. They get both the moment they sign in with that address."
          />
          <Pillar variant="card"
            icon={<UserX />}
            heading="Suspension"
            description="Switch someone off everywhere at once: every app and the workspace close to them, not one by one."
          />
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="Roles, per app"
          description="Given to a team or a person, on each app. Most of the company only ever needs the first two."
        />
        <Card className={cn(glass, infoTable, "overflow-x-auto p-0")}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Someone who…</TableHead>
                <TableHead>What that means</TableHead>
                <TableHead className="text-right">Called</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {roles.map((r) => (
                <TableRow key={r.role}>
                  <TableCell className="font-medium">{r.plain}</TableCell>
                  <TableCell className="whitespace-normal text-muted-foreground">{r.detail}</TableCell>
                  <TableCell className="text-right font-mono text-xs">{r.role}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="Checked at the door"
          description="Who may open an app is decided before a request reaches it. Everyone else is asked to sign in, or told which team the app is for. The app gets no sign-in code to get wrong: it is told who is there."
        />
        <CodeWindow
          className="mx-auto w-full max-w-xl"
          title="Request headers"
          icon={<FileCode />}
          language="txt"
          code={`X-Shpyrd-User:   luis@acme.com
X-Shpyrd-Name:   Luís Pereira
X-Shpyrd-Teams:  finance,everyone
X-Shpyrd-Roles:  user`}
        />
      </Stack>

    </PageLayoutContent>
  );
}
