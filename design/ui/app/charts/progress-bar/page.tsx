import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="How far it has gone">
        <Stack gap="cozy" className="max-w-sm">
          <ProgressBar label="Build" value={25} />
          <ProgressBar label="Upload" value={62} />
          <ProgressBar label="Release" value={100} tone="success" />
        </Stack>
      </Section>
      <Section title="Sizes">
        <Stack gap="cozy" className="max-w-sm">
          <ProgressBar aria-label="Small" value={40} size="sm" />
          <ProgressBar aria-label="Default" value={40} />
          <ProgressBar aria-label="Large" value={40} size="lg" />
        </Stack>
      </Section>
      <Section title="Of what parts it is made">
        <Stack gap="normal" className="max-w-sm">
          <ProgressBar
            label="Disk"
            max={200}
            format={(v) => `${v} GiB`}
            segments={[
              { label: "Images", value: 84 },
              { label: "Volumes", value: 52 },
              { label: "Logs", value: 21 },
            ]}
          />
          <ProgressBar
            label="Tests"
            max={128}
            segments={[
              { label: "Passed", value: 117, tone: "success" },
              { label: "Skipped", value: 6, tone: "warning" },
              { label: "Failed", value: 3, tone: "error" },
            ]}
          />
        </Stack>
      </Section>
      <Section title="In a line of text">
        <p className="text-sm">
          The release is <ProgressBar inline aria-label="Release" value={62} /> 62% done, and
          the tests <ProgressBar inline aria-label="Tests" value={91} tone="success" /> almost.
        </p>
      </Section>
      <Section title="With another ink">
        <Stack gap="cozy" className="max-w-sm">
          <ProgressBar label="Blue" value={55} tone="blue" />
          <ProgressBar label="Warning" value={78} tone="warning" />
          <ProgressBar label="Error" value={93} tone="error" />
        </Stack>
      </Section>
    </>
  );
}
