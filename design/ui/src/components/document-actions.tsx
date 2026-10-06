"use client";
import * as React from "react";
import { Check, Download, Link2, Printer } from "lucide-react";
import { Button } from "./button";
import { Input } from "./input";

export type DocumentActionsLabels = { copy: string; copied: string; copyFailed: string; download: string; print: string };
const defaults: DocumentActionsLabels = { copy: "Copy version link", copied: "Link copied", copyFailed: "Could not copy. Use this version link:", download: "Download text", print: "Print" };
// The application owns URLs, localization and document generation.
function DocumentActions({ permalink, downloadHref, onPrint, labels = defaults, className }: {
  permalink: string; downloadHref?: string; onPrint?: () => void; labels?: DocumentActionsLabels; className?: string;
}) {
  const id = React.useId();
  const [copied, setCopied] = React.useState(false);
  const [fallback, setFallback] = React.useState("");
  const request = React.useRef(0);
  React.useEffect(() => { request.current++; setCopied(false); setFallback(""); return () => { request.current++; }; }, [permalink]);
  async function copyLink() {
    const attempt = ++request.current;
    const url = new URL(permalink, window.location.href).href;
    try { await navigator.clipboard.writeText(url); if (attempt === request.current) { setCopied(true); setFallback(""); } }
    catch { if (attempt === request.current) { setCopied(false); setFallback(url); } }
  }
  return <div data-slot="document-actions" className={className}><div className="flex flex-wrap items-center gap-1.5"><Button variant="outline" size="sm" icon={copied ? <Check /> : <Link2 />} onClick={copyLink}>{copied ? labels.copied : labels.copy}</Button>{downloadHref && <Button variant="ghost" size="sm" asChild icon={<Download />}><a href={downloadHref} download>{labels.download}</a></Button>}<Button variant="ghost" size="sm" icon={<Printer />} onClick={onPrint ?? (() => window.print())}>{labels.print}</Button></div><span className="sr-only" role="status">{copied ? labels.copied : ""}</span>{fallback && <div className="mt-3 text-xs"><label htmlFor={id}>{labels.copyFailed}</label><Input id={id} className="mt-2" readOnly value={fallback} onFocus={event => event.target.select()} /></div>}</div>;
}
export { DocumentActions };
