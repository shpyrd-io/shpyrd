import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default, in a frame">
        <div className="rounded-xl border bg-card">
          <Table>
            {head}
            <TableBody>{rows}</TableBody>
            <TableFooter>{total}</TableFooter>
          </Table>
        </div>
      </Section>
      <Section title="Secondary: lines only between the rows">
        <Table variant="secondary">
          {head}
          <TableBody>{rows}</TableBody>
          <TableFooter>{total}</TableFooter>
        </Table>
      </Section>
    </>
  );
}

const head = (
  <TableHeader>
    <TableRow>
      <TableHead>Project</TableHead>
      <TableHead>Status</TableHead>
      <TableHead className="text-right">Release</TableHead>
    </TableRow>
  </TableHeader>
);

const rows = [
  ["Hello World", "Running", "v12"],
  ["Docs 001", "Running", "v4"],
  ["hello", "Stopped", "v1"],
].map(([project, status, release]) => (
  <TableRow key={project}>
    <TableCell>{project}</TableCell>
    <TableCell>
      <StatusBadge type={status === "Running" ? "success" : "neutral"}>{status}</StatusBadge>
    </TableCell>
    <TableCell className="text-right font-mono text-xs">{release}</TableCell>
  </TableRow>
));

const total = (
  <TableRow>
    <TableCell colSpan={2}>Projects</TableCell>
    <TableCell className="text-right">3</TableCell>
  </TableRow>
);
