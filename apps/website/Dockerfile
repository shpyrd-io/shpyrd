# The website, shpyrd.io, on shpyrd (apps/website/shpyrd.yaml). Built from
# the repository's root: the site needs the workspace's design/ui and
# content. Its output is Next's standalone server (next.config.ts).
FROM node:24-slim AS build
WORKDIR /src
COPY . .
# Public: Google Tag Manager's container is in every page's source.
ARG NEXT_PUBLIC_GTM_ID
ENV NEXT_TELEMETRY_DISABLED=1 NEXT_PUBLIC_GTM_ID=$NEXT_PUBLIC_GTM_ID
RUN npm ci --no-audit --no-fund --workspace apps/website --include-workspace-root=false
RUN npm run build --workspace apps/website

FROM node:24-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=8080
# The standalone server, and what it does not carry itself: the browser's
# files and the public folder.
COPY --from=build --chown=1000:1000 /src/apps/website/.next/standalone ./
COPY --from=build --chown=1000:1000 /src/apps/website/.next/static ./apps/website/.next/static
COPY --from=build --chown=1000:1000 /src/apps/website/public ./apps/website/public
# Numeric: Kubernetes verifies non-root users by uid (shpyrd runs every
# process as non-root).
USER 1000
EXPOSE 8080
CMD ["node", "apps/website/server.js"]
