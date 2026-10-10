// A page that says what a sibling package of the workspace built.
const http = require("node:http");
const greeting = require("@monorepo/greeting");

http
  .createServer((_, res) => {
    res.setHeader("content-type", "text/html; charset=utf-8");
    res.end(`<h1>${greeting}</h1>\n`);
  })
  .listen(process.env.PORT || 8080);
