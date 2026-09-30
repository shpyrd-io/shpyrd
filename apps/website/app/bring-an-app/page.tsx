import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact, offer, secondaryCta } from "@shpyrd/content/site/offer";

export const metadata = {
  title: "Bring an app to a sharing session",
  description: offer.intro,
};

export default function BringAnApp() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Stack gap="normal">
        <h1 className="max-w-[18ch] text-4xl font-semibold tracking-tight">{offer.title}</h1>
        <p className="max-w-prose text-lg text-muted-foreground">{offer.intro}</p>
        <Button asChild className="justify-self-start">
          <a href={contact.href}>{contact.label}</a>
        </Button>
        <p className="text-sm text-muted-foreground">{contact.note}</p>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.bring.title}</h2>
        <dl className="grid gap-8 sm:grid-cols-3">
          {offer.bring.items.map((item) => (
            <div key={item.title}>
              <dt className="font-semibold">{item.title}</dt>
              <dd className="mt-2 text-muted-foreground">{item.body}</dd>
            </div>
          ))}
        </dl>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.happens.title}</h2>
        <ul className="grid max-w-prose gap-3">
          {offer.happens.items.map((item) => (
            <li key={item} className="flex gap-3">
              <span aria-hidden="true" className="mt-2.5 h-px w-4 shrink-0 bg-primary" />
              <span className="text-muted-foreground">{item}</span>
            </li>
          ))}
        </ul>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.scope.title}</h2>
        <dl className="grid gap-8 sm:grid-cols-3">
          {offer.scope.items.map((item) => (
            <div key={item.title}>
              <dt className="font-semibold">{item.title}</dt>
              <dd className="mt-2 text-muted-foreground">{item.body}</dd>
            </div>
          ))}
        </dl>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.poorFit.title}</h2>
        <p className="max-w-prose text-muted-foreground">{offer.poorFit.intro}</p>
        <ul className="grid max-w-prose gap-4">
          {offer.poorFit.items.map((item) => (
            <li key={item} className="border-l-2 pl-4 text-muted-foreground">
              {item}
            </li>
          ))}
        </ul>
        <p className="max-w-prose">If none of those describe you, bring the app.</p>
        <Stack direction="horizontal" gap="cozy">
          <Button asChild>
            <a href={contact.href}>{contact.label}</a>
          </Button>
          <Button variant="outline" asChild>
            <a href={secondaryCta.href}>{secondaryCta.label}</a>
          </Button>
        </Stack>
      </Stack>
    </PageLayoutContent>
  );
}
