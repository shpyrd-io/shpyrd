#!/usr/bin/env bash
# What an application of another repository sees of @shpyrd/ui: the packed
# library installed from its tarball into a Next application outside the
# workspace, built. It proves the exports, the peers, the compilation of the
# source and Tailwind's @source from node_modules: a component's class must
# be in the CSS the build writes.
set -euo pipefail

ui=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

tarball=$(cd "$ui" && npm pack --pack-destination "$work" --silent | tail -n 1)

# The versions the library develops against: the same an application uses.
dep() {
  node -e "const p = require('$ui/package.json'); console.log((p.devDependencies || {})['$1'] || (p.dependencies || {})['$1'])"
}

app="$work/app"
mkdir -p "$app/app"
cat > "$app/package.json" <<EOF
{
  "name": "consumer-check",
  "private": true,
  "type": "module",
  "dependencies": {
    "@shpyrd/ui": "file:$work/$tarball",
    "next": "$(dep next)",
    "react": "$(dep react)",
    "react-dom": "$(dep react-dom)",
    "tailwindcss": "$(dep tailwindcss)",
    "@tailwindcss/postcss": "$(dep @tailwindcss/postcss)"
  },
  "devDependencies": {
    "typescript": "$(dep typescript)",
    "@types/node": "$(dep @types/node)",
    "@types/react": "$(dep @types/react)",
    "@types/react-dom": "$(dep @types/react-dom)"
  }
}
EOF
cat > "$app/next.config.ts" <<'EOF'
const config = { transpilePackages: ["@shpyrd/ui"] };
export default config;
EOF
cat > "$app/postcss.config.mjs" <<'EOF'
export default { plugins: { "@tailwindcss/postcss": {} } };
EOF
cat > "$app/app/globals.css" <<'EOF'
@import "@shpyrd/ui/styles.css";
EOF
cat > "$app/app/layout.tsx" <<'EOF'
import "./globals.css";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
EOF
cat > "$app/app/page.tsx" <<'EOF'
import { Button } from "@shpyrd/ui/components/button";

export default function Page() {
  return <Button>Deploy</Button>;
}
EOF

cd "$app"
npm install --no-audit --no-fund --loglevel=error
NEXT_TELEMETRY_DISABLED=1 npx next build

# rounded-control is the library's own radius, used by Button: present only
# when Tailwind read the library's source in node_modules.
if ! grep -rqs "rounded-control" .next/static; then
  echo "the build has no rounded-control: Tailwind did not read @shpyrd/ui's classes" >&2
  exit 1
fi
echo "an application outside the workspace builds @shpyrd/ui with its styles"
