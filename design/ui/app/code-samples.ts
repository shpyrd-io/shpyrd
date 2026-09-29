// The code the gallery shows in the IDE.

export const manifest = `# What shpyrd runs for this project.
name: hello-world
processes:
  web:
    command: "node server.js"
    size: shared-s
    instances: 2
  worker:
    command: "node worker.js"
    instances: 1
`;

export const server = `import { createServer } from "node:http";

// The port comes from the platform.
const port = Number(process.env.PORT ?? 3000);

const server = createServer((request, response) => {
  if (request.url === "/healthz") {
    response.writeHead(200).end("ok");
    return;
  }
  response.writeHead(200, { "content-type": "text/plain" });
  response.end(\`Hello from \${process.env.SHPYRD_PROCESS}\\n\`);
});

server.listen(port, () => console.log("listening on", port));
`;

export const deploy = `# Sign in once, then deploy what is in this folder.
shpyrd login
shpyrd deploy --project hello-world
shpyrd logs --project hello-world --process web
`;

export const long = Array.from(
  { length: 40 },
  (_, i) => `log.info("step ${i + 1} of the release", { release: "v12", step: ${i + 1} });`,
).join("\n");
