import { describe, expect, it } from "vitest";
import { flow, governance, opening, problem, slides, xray } from "./tour";
import { companies } from "./home";

describe("the Small Software manifesto's copy", () => {
  it("names eleven slides, the first the brand's", () => {
    expect(slides).toHaveLength(11);
    expect(slides[0]).toBe("shpyrd");
  });

  it("counts the same company of 47 apps on every slide that counts it", () => {
    expect(problem.total).toBe(47);
    expect(opening.before).toContain("47");
    expect(governance.stats[0].value).toBe("47");
  });

  it("marks on the x-ray only what its columns name", () => {
    for (const row of xray.rows) for (const i of row.has) expect(i).toBeLessThan(xray.columns.length);
  });

  it("puts each message of the agent's conversation under a step that exists", () => {
    for (const m of flow.messages) expect(m.step).toBeLessThan(flow.steps.length);
    expect(flow.messages[0].from).toBe("person");
  });

  it("writes the name in lower case, everywhere it appears", () => {
    expect(JSON.stringify([slides, opening, problem, xray, flow, governance])).not.toMatch(/Shpyrd/);
  });

  it("is where the home page's business section sends people", () => {
    expect(companies.manifesto.href).toBe("/small-software-manifesto");
  });
});
