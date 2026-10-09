import { sales } from "@shpyrd/content/site/contact";
import { Contact } from "@/components/contact";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: sales.title,
  description: sales.description,
  ...shared(`${sales.title} · shpyrd`, sales.description),
};

export default function Page() {
  return <Contact kind="sales" />;
}
