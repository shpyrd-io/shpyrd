"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";

// A line to copy: a sentence for the agent, or a command. Owed to
// design/ui once it settles.
export function CopyLine({ icon, text, mono = false }: { icon?: React.ReactNode; text: string; mono?: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-muted/40 py-2 pr-2 pl-3 [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-muted-foreground">
      {icon}
      <span className={mono ? "min-w-0 flex-1 truncate font-mono text-xs" : "min-w-0 flex-1 text-sm"}>{mono ? text : `“${text}”`}</span>
      <Button
        variant="outline"
        size="sm"
        icon={copied ? <Check /> : <Copy />}
        onClick={() => {
          void navigator.clipboard?.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}
