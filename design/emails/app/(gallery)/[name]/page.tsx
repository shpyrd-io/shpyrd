import { notFound } from "next/navigation";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { mails, subjectOf } from "@/src/emails";
import { Frame } from "../frame";
import { Section } from "../section";

// One email: what the inbox shows, the email wide and on a phone, and the
// words the server fills in.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(mails).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const { name } = await params;
  const mail = mails[name];
  if (!mail) notFound();
  return (
    <>
      <Section title="In the inbox">
        <InfoTable layout="rows" columns={1} className="max-w-2xl">
          <InfoTableItem label="Subject">{subjectOf(mail)}</InfoTableItem>
          <InfoTableItem label="Template">
            <a href={`/template/${name}`} className="text-primary underline-offset-4 hover:underline">
              /template/{name}
            </a>
          </InfoTableItem>
        </InfoTable>
      </Section>
      <Section title="Wide, and on a phone">
        <div className="flex flex-wrap items-start gap-6">
          <Frame src={`/preview/${name}`} width={640} title={`${mail.title}, wide`} />
          <Frame src={`/preview/${name}`} width={375} title={`${mail.title}, on a phone`} />
        </div>
      </Section>
      <Section title="The words the server fills in">
        <InfoTable layout="rows" columns={1} className="max-w-2xl">
          {Object.entries(mail.sample).map(([k, v]) => (
            <InfoTableItem key={k} label={<code>{`{{.${k}}}`}</code>}>
              {v}
            </InfoTableItem>
          ))}
        </InfoTable>
      </Section>
    </>
  );
}
