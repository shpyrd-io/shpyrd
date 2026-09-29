import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Types">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <StatusBadge type="success">Running</StatusBadge>
          <StatusBadge type="info" live>
            Building
          </StatusBadge>
          <StatusBadge type="warning" live>
            Deploying
          </StatusBadge>
          <StatusBadge type="neutral">Pending</StatusBadge>
          <StatusBadge type="error">Failed</StatusBadge>
        </Stack>
      </Section>
      <Section title="With a quantity">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <StatusBadge type="success" qty="2/2">
            Running
          </StatusBadge>
          <StatusBadge type="warning" qty="1/2" live>
            Deploying
          </StatusBadge>
        </Stack>
      </Section>
      <Section title="Secondary, for names">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <StatusBadge variant="secondary" type="success" qty="1/1">
            web
          </StatusBadge>
          <StatusBadge variant="secondary" type="warning" qty="1/2" live>
            worker
          </StatusBadge>
          <StatusBadge variant="secondary" type="error" qty="0/1">
            cron
          </StatusBadge>
          <StatusBadge variant="secondary" type="neutral" qty="0/0">
            jobs
          </StatusBadge>
          <StatusBadge variant="secondary" type="info">
            release
          </StatusBadge>
        </Stack>
      </Section>
    </>
  );
}
