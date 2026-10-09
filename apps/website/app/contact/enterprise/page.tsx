import { enterprise } from "@shpyrd/content/site/contact";
import { Contact } from "@/components/contact";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: enterprise.title,
  description: enterprise.description,
  ...shared(`${enterprise.title} · shpyrd`, enterprise.description),
};

export default function Page() {
  return <Contact kind="enterprise" />;
}
