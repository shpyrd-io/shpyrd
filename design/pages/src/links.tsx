import { Button } from "@shpyrd/ui/components/button";

// The links of a page, as the server writes them: a Go template ranges
// over them around one button. The text around the button is that of the
// template, and reaches the file as it is.
export function TemplateLinks() {
  return (
    <>
      {"{{range .Links}}"}
      <Button asChild variant="outline" size="lg">
        <a href="{{.URL}}">{"{{.Label}}"}</a>
      </Button>
      {"{{end}}"}
    </>
  );
}

// The same links with sample words, for the gallery.
export function SampleLinks({ links }: { links: [string, string][] }) {
  return (
    <>
      {links.map(([label, url]) => (
        <Button key={label} asChild variant="outline" size="lg">
          <a href={url}>{label}</a>
        </Button>
      ))}
    </>
  );
}
