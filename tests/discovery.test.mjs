import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);

test("publishes a crawler-safe GEO surface", async () => {
  const [robots, sitemap, llms] = await Promise.all([
    readFile(new URL("public/robots.txt", root), "utf8"),
    readFile(new URL("public/sitemap.xml", root), "utf8"),
    readFile(new URL("public/llms.txt", root), "utf8"),
  ]);

  assert.match(robots, /Sitemap:\s+https:\/\/owu\.terracat\.net\/sitemap\.xml/);
  assert.match(robots, /Disallow: \/browse\//);
  assert.match(robots, /Disallow: \/socket\//);
  assert.match(robots, /Disallow: \/tunnel\//);
  assert.doesNotMatch(sitemap, /\/stats/);
  assert.match(sitemap, /<loc>https:\/\/owu\.terracat\.net\/<\/loc>/);
  assert.match(llms, /https:\/\/github\.com\/TypeThe0ry\/OWU/);
  assert.match(llms, /GitHub OAuth or generic OIDC SSO/);
});

test("ships the SSO edge without repository secrets", async () => {
  const [env, service, httpConfig, serverConfig, docs] = await Promise.all([
    readFile(new URL("deploy/oauth2-proxy.env.example", root), "utf8"),
    readFile(new URL("deploy/oauth2-proxy.service", root), "utf8"),
    readFile(new URL("deploy/nginx-owu-sso-http.conf", root), "utf8"),
    readFile(new URL("deploy/nginx-owu-sso-server.conf", root), "utf8"),
    readFile(new URL("docs/SSO-GEO.md", root), "utf8"),
  ]);

  assert.match(env, /OAUTH2_PROXY_PROVIDER=github/);
  assert.match(env, /OAUTH2_PROXY_CLIENT_SECRET_FILE=/);
  assert.match(env, /OAUTH2_PROXY_COOKIE_SECRET_FILE=/);
  assert.doesNotMatch(env, /ghp_[A-Za-z0-9]{20,}/);
  assert.match(service, /EnvironmentFile=\/etc\/owu\/oauth2-proxy\.env/);
  assert.match(httpConfig, /127\.0\.0\.1:4180/);
  assert.match(serverConfig, /auth_request \/oauth2\/auth/);
  assert.match(serverConfig, /location \^~ \/oauth2\//);
  assert.match(docs, /GitHub SSO/);
  assert.match(docs, /Generic OIDC/);
});

test("emits machine-readable OWU, source, and maintainer entities", async () => {
  const layout = await readFile(new URL("app/layout.tsx", root), "utf8");
  assert.match(layout, /SoftwareSourceCode/);
  assert.match(layout, /WebApplication/);
  assert.match(layout, /https:\/\/github\.com\/TypeThe0ry\/OWU/);
  assert.match(layout, /Team TerraCat/);
  assert.match(layout, /"@graph"/);
});
