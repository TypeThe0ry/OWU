import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { checkAccess, normalizeInputUrl, safeLaunchUrl } from "../app/access-check.mjs";

test("normalizes a bare HTTPS host and preserves its port and path", () => {
  const result = normalizeInputUrl("  EXAMPLE.com:8443/dashboard?q=ok  ");
  assert.equal(result.ok, true);
  assert.equal(result.inputUrl, "https://example.com:8443/dashboard?q=ok");
  assert.deepEqual(result.preview, { host: "example.com", port: "8443", protocol: "HTTPS" });
});

test("rejects unsupported schemes and embedded credentials before access checking", () => {
  assert.deepEqual(normalizeInputUrl("ftp://example.com"), { ok: false, reason: "unsupported_scheme" });
  assert.deepEqual(normalizeInputUrl("https://user:pass@example.com"), { ok: false, reason: "embedded_credentials" });
});

test("posts only input_url to the same-origin access check", async () => {
  let request;
  const result = await checkAccess({
    inputUrl: "https://example.com/",
    fetchImpl: async (url, init) => {
      request = { url, init };
      return new Response(JSON.stringify({
        decision: "allowed",
        launch_url: "https://r-safe.access.example.test/_launch/opaque",
      }), { status: 200 });
    },
  });
  assert.equal(request.url, "/api/access/check");
  assert.equal(request.init.method, "POST");
  assert.equal(request.init.credentials, "same-origin");
  assert.deepEqual(JSON.parse(request.init.body), { input_url: "https://example.com/" });
  assert.deepEqual(Object.keys(request.init.headers), ["Content-Type"]);
  assert.equal(result.decision, "allowed");
});

test("maps all denied states without returning a launch URL", async () => {
  for (const decision of ["resource_not_authorized", "blocked", "port_not_allowed", "rate_limited"]) {
    const result = await checkAccess({
      inputUrl: "https://example.com/",
      fetchImpl: async () => new Response(JSON.stringify({ decision }), {
        status: decision === "rate_limited" ? 429 : 403,
        headers: { "retry-after": "12" },
      }),
    });
    assert.equal(result.decision, decision);
    assert.equal(result.launchUrl, undefined);
    if (decision === "rate_limited") assert.equal(result.retryAfterSeconds, 12);
  }
});

test("fails closed on network errors, unknown responses, and allowed responses without a launch URL", async () => {
  const failures = [
    async () => { throw new Error("offline"); },
    async () => new Response(JSON.stringify({ decision: "mystery" }), { status: 500 }),
    async () => new Response(JSON.stringify({ decision: "allowed" }), { status: 200 }),
  ];
  for (const fetchImpl of failures) {
    const result = await checkAccess({ inputUrl: "https://example.com/", fetchImpl });
    assert.equal(result.decision, "service_error");
  }
});

test("accepts an HTTPS gateway launch and rejects insecure destinations", () => {
  assert.equal(
    safeLaunchUrl("https://r-safe.access.example.test/_launch/opaque", "https://permit.example.test"),
    "https://r-safe.access.example.test/_launch/opaque",
  );
  assert.equal(safeLaunchUrl("http://example.com/", "https://permit.example.test"), null);
  assert.equal(safeLaunchUrl("https://example.com/dashboard", "https://permit.example.test"), null);
  assert.equal(safeLaunchUrl("javascript:alert(1)", "https://permit.example.test"), null);
});

test("keeps the public UI to one access form without account or authorization controls", async () => {
  const source = await readFile(new URL("../app/page.tsx", import.meta.url), "utf8");
  assert.match(source, /id="target-url"/);
  assert.match(source, /Visit website/);
  assert.doesNotMatch(source, /type="checkbox"|>\s*Sign in\s*<|>\s*Register\s*<|Create account|Mac app/i);
});

test("the same-origin BFF fails closed when no control API is configured", async () => {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);
  const response = await worker.fetch(
    new Request("http://localhost/api/access/check", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ input_url: "https://example.com/" }),
    }),
    { ASSETS: { fetch: async () => new Response("Not found", { status: 404 }) } },
    { waitUntil() {}, passThroughOnException() {} },
  );
  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), { decision: "service_error" });
});
