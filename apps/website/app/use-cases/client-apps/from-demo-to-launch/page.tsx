import { Eye, Rocket, Undo2, Users } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ClientAppsStep } from "@/components/use-case-client-apps";
import { VerticalRoute } from "@/components/vertical-route";
import { pageSections } from "@/lib/page";

// Client apps · Demo to day one: the life of one app inside the engagement,
// on one address, as a timeline. Each step is a grant or a release; nothing
// is rebuilt.

export const metadata = {
  title: "Client apps · From demo to launch",
  description:
    "One address for the client's app, from the first demo to the day their team starts using it, and every fix after.",
};

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero align="center"
        variant="page"
        heading="From the first demo to launch, on one address."
        description="The app you show the client on Tuesday is the one their team uses next month, on shpyrd cloud or in their own cloud. In between, only who can open it changes."
      />

      {/* The steps as the home page's route, standing up (vertical-route.tsx). */}
      <VerticalRoute
        className="mx-auto w-full max-w-2xl"
        steps={[
          {
            icon: <Rocket />,
            children: (
              <p>
                <strong>The first demo.</strong> You deploy it to the client&apos;s workspace. It has
                its address and sign-in from the start, and only your team can open it.
              </p>
            ),
          },
          {
            icon: <Eye />,
            children: (
              <p>
                <strong>The client tries it.</strong> You add the three people who will sign it off, by
                email. They get in with their own work account.
              </p>
            ),
          },
          {
            icon: <Users />,
            lit: true,
            children: (
              <p>
                <strong>Go live.</strong> You give it to their Operations team. Everyone in that group
                can open it that morning, and nobody else.
              </p>
            ),
          },
          {
            icon: <Undo2 />,
            children: (
              <p>
                <strong>The first fix.</strong> Every deploy is a numbered release. When one goes wrong,
                you put the previous one back, code and settings together, while you look.
              </p>
            ),
          },
        ]}
      />

      <ClientAppsStep next={{ href: "/use-cases/client-apps/the-delivery-sheet", label: "The delivery sheet" }} />
    </PageLayoutContent>
  );
}
