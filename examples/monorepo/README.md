# A pnpm workspace

`apps/web` serves a page with what `packages/greeting` builds. Deploy the
app from its own folder; shpyrd finds the workspace above it, uploads the
whole of it, and builds `apps/web` there:

    cd apps/web && shpyrd deploy
