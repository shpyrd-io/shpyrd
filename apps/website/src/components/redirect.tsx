"use client";

import * as React from "react";

// An address that moved: it takes whoever has it to where the page is now.
// The page is static, so it goes in the browser (and by a refresh tag for a
// reader without script).
export function Redirect({ to }: { to: string }) {
  React.useEffect(() => {
    window.location.replace(to);
  }, [to]);
  return (
    <>
      <meta httpEquiv="refresh" content={`0; url=${to}`} />
      <p className="p-8 text-center text-muted-foreground">
        This page has moved to <a href={to} className="text-foreground underline underline-offset-4">a new address</a>.
      </p>
    </>
  );
}
