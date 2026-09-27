/** The golden table lives with the Go parser, in pkg/logfmt/testdata, and is
 * read here as well as by pkg/logfmt's TestGoldenCases. One set of answers
 * holds both parsers to the same contract, so a change that moves one and not
 * the other fails in one of the two suites rather than drifting silently —
 * which is what the two hand-mirrored test tables could not guarantee.
 *
 * Regenerate the file deliberately when the contract is meant to change. */
// Imported with Vite's ?raw so the app's tsconfig needs no node types; the
// path reaches out of ui/ on purpose, because the table belongs with the Go
// parser rather than being copied here to drift.
import table from "../../../pkg/logfmt/testdata/cases.jsonl?raw";
import { describe, expect, it } from "vitest";
import { parseLogLine } from "@/lib/logs";

type Golden = {
  line: string;
  structured: boolean;
  level: string;
  levelText: string;
  message: string;
  time: string;
  fields: [string, string][];
};

const cases: Golden[] = table
  .split("\n")
  .filter((line) => line.trim() !== "")
  .map((line) => JSON.parse(line) as Golden);

describe("parseLogLine against the shared golden table", () => {
  it("reads the table", () => {
    expect(cases.length).toBeGreaterThan(0);
  });

  it.each(cases.map((c, i) => [i + 1, c] as const))(
    "case %i agrees with pkg/logfmt",
    (_n, c) => {
      const l = parseLogLine(c.line);
      expect({
        structured: l.structured,
        level: l.level,
        levelText: l.levelText,
        message: l.message,
        time: l.time ?? "",
        fields: l.fields.map((f) => [f.key, f.value]),
      }).toEqual({
        structured: c.structured,
        level: c.level,
        levelText: c.levelText,
        message: c.message,
        time: c.time,
        fields: c.fields,
      });
    },
  );
});
