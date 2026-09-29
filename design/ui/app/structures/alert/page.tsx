import { Info } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Section } from "../../section";

export default function Page() {
  return (
    <Section title="Default">
      <Alert>
        <Info />
        <AlertTitle>The cluster is being upgraded</AlertTitle>
        <AlertDescription>Deploys wait until it ends.</AlertDescription>
      </Alert>
    </Section>
  );
}
