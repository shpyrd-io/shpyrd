import { Lock, Rocket } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { Section } from "../../section";

// A whole page that says one thing, in the plain style: the mark, the
// words in the middle, the wordmark in the corner. Four things a page
// may say: nothing answers here yet, this is not for you, wait a moment,
// nothing can answer right now; and with nothing to say, the mark alone. The server draws these pages
// from the same component (design/pages).

export default function Page() {
  return (
    <>
      <Section title="Nothing here, yet">
        <StatePage
          className="min-h-[26rem] rounded-xl ring-1 ring-foreground/10"
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
        <StatePage
          className="min-h-[26rem] rounded-xl ring-1 ring-foreground/10"
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
        <StatePage
          className="min-h-[26rem] rounded-xl ring-1 ring-foreground/10"
          icon={<Rocket />}
          waiting
          title="Waking the application up."
          description="It was asleep, as it is when nobody asks for it. It answers in a few seconds; this page goes to it by itself."
        />
      </Section>
      <Section title="Service unavailable: a picture in the place of the icon">
        <StatePage
          className="min-h-[34rem] rounded-xl ring-1 ring-foreground/10"
          picture={<Shipyard className="w-80 max-w-[80vw]" />}
          title="Service unavailable"
          description="This is not available right now. Try again in a few moments."
        />
      </Section>
      <Section title="Nothing to say: the mark alone">
        <StatePage className="min-h-[26rem] rounded-xl ring-1 ring-foreground/10" />
      </Section>
    </>
  );
}
