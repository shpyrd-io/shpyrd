import { Card, CardContent, CardHeader } from "@shpyrd/ui/components/card";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

// The shape of what is coming, while it comes: a grey block where each
// thing will be, about the size it will have.
export default function Page() {
  return (
    <>
      <Section title="Lines of text">
        <Stack gap="condensed" className="max-w-sm">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-1/2" />
        </Stack>
      </Section>
      <Section title="A heading and its page">
        <Stack gap="normal">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-4 w-96" />
          <Skeleton className="h-40 w-full" />
        </Stack>
      </Section>
      <Section title="A card">
        <Card className="max-w-sm">
          <CardHeader>
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-4 w-56" />
          </CardHeader>
          <CardContent>
            <Stack gap="condensed">
              {[0, 1, 2].map((i) => (
                <Stack key={i} direction="horizontal" align="center" gap="cozy">
                  <Skeleton className="size-8 rounded-full" />
                  <Stack gap="tight" className="flex-1">
                    <Skeleton className="h-3.5 w-1/2" />
                    <Skeleton className="h-3 w-1/3" />
                  </Stack>
                </Stack>
              ))}
            </Stack>
          </CardContent>
        </Card>
      </Section>
      <Section title="The rows of a table">
        <Stack gap="condensed">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </Stack>
      </Section>
    </>
  );
}
