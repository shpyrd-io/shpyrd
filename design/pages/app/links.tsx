import { Button } from "@shpyrd/ui/components/button";

// The links of a page, as the server writes them: a Go template ranges
// over them around one button. The text around the button is that of the
// template, and reaches the file as it is.
export function Links() {
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
