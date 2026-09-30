import { Loader2, Lock, Rocket } from "lucide-react";
import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Section } from "../../section";

// A whole page that says one thing, in the plain style: the mark, the
// words in the middle, the wordmark in the corner. Three things a page
// may say: nothing answers here yet, this is not for you, wait a moment.

export default function Page() {
  return (
    <>
      <Section title="Nothing here, yet">
        <State
          title="Nothing here, yet."
          description="No application answers at this address. Deploy one and it will."
          action={
            <Button variant="outline" size="lg">
              Deploy something
            </Button>
          }
        />
      </Section>
      <Section title="No access">
        <State
          icon={<Lock />}
          title="This application is not open to you."
          description="hello.acme.shpyrd.app lets in the people its project names. Ask someone of the project, or sign in with another account."
          action={
            <>
              <Button variant="outline" size="lg">
                Sign in with another account
              </Button>
              <Button variant="ghost" size="lg">
                Ask for access
              </Button>
            </>
          }
        />
      </Section>
      <Section title="Waking up">
        <State
          icon={<Rocket />}
          waiting
          title="Waking the application up."
          description="It was asleep, as it is when nobody asks for it. It answers in a few seconds; this page goes to it by itself."
        />
      </Section>
    </>
  );
}

function State({
  icon,
  title,
  description,
  action,
  waiting = false,
}: {
  // Over the title: the mark, or an icon that says what happened.
  icon?: React.ReactElement;
  title: string;
  description: string;
  action?: React.ReactNode;
  // Something is on its way: a spinner turns under the words.
  waiting?: boolean;
}) {
  return (
    <div className="relative flex min-h-[26rem] items-center justify-center overflow-hidden rounded-xl bg-background ring-1 ring-foreground/10">
      <div className="grid justify-items-center gap-5 px-6 text-center">
        {icon ? (
          <span className="inline-flex size-16 items-center justify-center rounded-2xl bg-muted text-muted-foreground [&_svg]:size-8">
            {icon}
          </span>
        ) : (
          <LogoMark className="size-16" />
        )}
        <h2 className="font-heading text-2xl font-medium">{title}</h2>
        <p className="max-w-md text-sm text-balance text-muted-foreground">{description}</p>
        {waiting && (
          <Loader2 aria-label="Waiting" className="size-5 animate-spin text-muted-foreground" />
        )}
        {action && <div className="flex flex-wrap items-center justify-center gap-2">{action}</div>}
      </div>
      <Wordmark className="absolute bottom-5 left-5 h-5 opacity-60" />
    </div>
  );
}
