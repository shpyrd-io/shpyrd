import { Button } from "@shpyrd/ui/components/button";
import { ShipyardCta } from "@/components/shipyard-cta";
import { SubpageStep } from "@/components/subpage-step";
import { signUp } from "@/lib/signup";

// What the pages of "For developers" share: the overview's ending, and the
// short step that closes each subpage. shpyrd cloud comes first everywhere;
// running it yourself has a tab of its own, the last.
//
// "Get started" is the self sign-up of shpyrd cloud (src/lib/signup.ts).

// The end of the overview: sign up and deploy, with the yard beside it.
export function DeveloperNext() {
  return (
    <ShipyardCta
      heading="Start on shpyrd cloud"
      description="Sign up and your workspace is ready, with nothing to install or run. The first deploy takes the CLI and one folder."
      actions={
        <>
          <Button size="lg" asChild>
            <a href={signUp}>Get started</a>
          </Button>
          <Button variant="outline" size="lg" asChild>
            <a href="/for/developers#your-first-deploy">Quick start</a>
          </Button>
          <Button variant="ghost" size="lg" asChild>
            <a href="https://github.com/shpyrd-io/shpyrd">Read the source</a>
          </Button>
        </>
      }
      note={
        <>
          Open source under MPL-2.0: you can also{" "}
          <a href="/for/developers#run-it-yourself" className="underline underline-offset-4 hover:text-foreground">
            run it yourself
          </a>
          .
        </>
      }
    />
  );
}

// The end of a subpage: the next one in order, and at most one thing to do.
export function DeveloperStep({
  next,
  action,
}: {
  next: { href: string; label: string };
  action?: { href: string; label: string };
}) {
  return (
    <SubpageStep
      action={
        action && (
          <Button asChild>
            <a href={action.href}>{action.label}</a>
          </Button>
        )
      }
      next={{ href: next.href, title: next.label }}
    />
  );
}

// The output of a deploy, as the CLI prints it (docs/deploying), shortened, at
// an address of a workspace on shpyrd cloud.
export const deployOutput = `$ shpyrd deploy
==> Archiving HEAD (654f4925638e)
==> Uploading source (2.6 KiB)
==> Building
===> detect
4 of 9 buildpacks participating
===> export
==> Releasing
    Running: web 3/3 · worker 1/1
Released v3: Deploy 654f4925638e
https://shop.acme.shpyrd.app`;
