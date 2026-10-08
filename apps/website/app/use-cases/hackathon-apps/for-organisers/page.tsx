import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { contact } from "@shpyrd/content/site/offer";
import { HackathonStep } from "@/components/use-case-hackathon-apps";
import { pageSections } from "@/lib/page";

// Apps from a hackathon - for the organiser. The person who ran the week, and
// wants it to have been worth it. The research's figure is used as the research
// uses it: a vendor-published story, and a count that is not value.

export const metadata = { title: "Apps from a hackathon · For organisers" };

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero align="center"
        variant="page"
        heading="You ran the hackathon. Make the good apps stick."
        description="The week is judged by what people still use a month later."
      />

      <p className="mx-auto max-w-prose border-l-2 pl-4 text-muted-foreground">
        In one story Replit published, a 24-hour hackathon with more than 700 employees produced 135
        apps. That count tells you people can build. It doesn&apos;t tell you which apps stayed in use.
        The work after the week is finding the few that matter and putting those in front of their
        users.
      </p>

      <p className="mx-auto max-w-prose text-center text-muted-foreground">
        Each builder shares their own, so nobody waits on you to hand out a link. As the
        workspace&apos;s owner or admin, you see every app in it, and who can open or change each one.
      </p>

      <HackathonStep
        action={
          <Button asChild>
            <a href={contact.href}>Talk to us about the apps worth keeping</a>
          </Button>
        }
        next={{ title: "Overview", href: "/use-cases/hackathon-apps" }}
      />
    </PageLayoutContent>
  );
}
