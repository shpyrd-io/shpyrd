import Link from "next/link";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { groups, mails, subjectOf } from "@/src/emails";
import { Frame } from "./frame";
import { Section } from "./section";

// The way in: every email, small, under the group that sends it.
export default function Overview() {
  return (
    <>
      <PageHeading title="Overview" description="Every email shpyrd sends, as a person gets it, with sample words." />
      {groups.map((group) => (
        <Section key={group} title={group === "Cloud" ? "Sent by shpyrd cloud" : "Sent by every platform"}>
          <div className="grid gap-6 @3xl/page-layout:grid-cols-2 @7xl/page-layout:grid-cols-3">
            {Object.entries(mails)
              .filter(([, mail]) => mail.group === group)
              .map(([name, mail]) => (
                <Link key={name} href={`/${name}`} className="group grid content-start gap-2">
                  <div className="pointer-events-none h-80 overflow-hidden rounded-lg border transition-colors group-hover:border-primary">
                    <div className="w-[200%] origin-top-left scale-50">
                      <Frame src={`/preview/${name}`} width={640} title={mail.title} />
                    </div>
                  </div>
                  <div className="text-sm font-medium">{mail.title}</div>
                  <div className="text-xs text-muted-foreground">{subjectOf(mail)}</div>
                </Link>
              ))}
          </div>
        </Section>
      ))}
    </>
  );
}
