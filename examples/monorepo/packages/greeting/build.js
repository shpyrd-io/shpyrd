// The package's build: what apps/web imports exists only after it runs.
const fs = require("node:fs");
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync("dist/index.js", 'module.exports = "Hello from a workspace package";\n');
