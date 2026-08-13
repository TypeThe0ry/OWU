import { createServer } from "node:http";

const port = Number.parseInt(process.env.PORT ?? "9000", 10);

const server = createServer((request, response) => {
  const requestUrl = new URL(request.url ?? "/", `http://${request.headers.host ?? "demo-target"}`);

  if (requestUrl.pathname === "/health") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ status: "ok" }));
    return;
  }

  if (requestUrl.pathname === "/api/whoami") {
    response.writeHead(200, {
      "cache-control": "no-store",
      "content-type": "application/json",
    });
    response.end(
      JSON.stringify({
        service: "Permit authorized demo target",
        method: request.method,
        path: requestUrl.pathname,
      }),
    );
    return;
  }

  response.writeHead(200, {
    "cache-control": "no-store",
    "content-type": "text/html; charset=utf-8",
    "set-cookie": "permit_demo=authorized; HttpOnly; SameSite=Lax; Path=/",
  });
  response.end(`<!doctype html>
<html lang="en">
  <head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Permit demo target</title></head>
  <body style="font-family:system-ui;max-width:48rem;margin:4rem auto;padding:0 1rem">
    <p>AUTHORIZED DEMO RESOURCE</p>
    <h1>The controlled route is working.</h1>
    <p>This page is served by the resource registered in the local Permit demo policy.</p>
    <a href="/api/whoami">Open the same-origin API fixture</a>
  </body>
</html>`);
});

server.listen(port, "0.0.0.0", () => {
  console.log(JSON.stringify({ event: "demo_target_listening", port }));
});

function shutdown() {
  server.close(() => process.exit(0));
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
