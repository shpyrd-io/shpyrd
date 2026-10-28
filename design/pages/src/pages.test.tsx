import { renderToStaticMarkup } from "react-dom/server";
import { expect, test } from "vitest";
import { marks, pages } from "./pages";

// A page's template, as the build writes it before scripts/pack.mjs.
const template = (name: string) => renderToStaticMarkup(<>{pages[name].render(marks())}</>);

test("a list writes its marks whether or not its sample has an item", () => {
  expect(pages.nothing.sample.Links).toEqual([]);
  expect(pages["no-access"].sample.Links).not.toEqual([]);
  for (const name of ["nothing", "no-access"]) {
    const html = template(name);
    expect(html).toContain("{{range .Links}}");
    expect(html).toContain('href="{{.URL}}"');
    expect(html).toContain("{{.Label}}</a>");
    expect(html).toContain("{{end}}");
  }
});

test("a word is its mark", () => {
  expect(template("nothing")).toContain("<h1");
  expect(template("nothing")).toContain("{{.Title}}");
  expect(template("nothing")).toContain("{{.Text}}");
});
