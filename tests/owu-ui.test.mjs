import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);

test("keeps the OWU home focused on one proxied URL form", async () => {
  const source = await readFile(new URL("app/page.tsx", root), "utf8");
  assert.match(source, /Open Website Unblocker/);
  assert.match(source, /id="website-url"/);
  assert.match(source, /Open website/);
  assert.match(source, /window\.location\.assign\(result\.url\)/);
  assert.match(source, /\/browse\/\$\{encodeOrigin\(target\.origin\)\}/);
  assert.equal((source.match(/<form\b/g) ?? []).length, 1);
  assert.equal((source.match(/<input\b/g) ?? []).length, 1);
  assert.doesNotMatch(source, /\/api\/access\/check|checkAccess|Sign in|Register|type="checkbox"/i);
});

test("accepts only browser-safe HTTP and HTTPS addresses", async () => {
  const source = await readFile(new URL("app/page.tsx", root), "utf8");
  assert.match(source, /url\.protocol !== "http:" && url\.protocol !== "https:"/);
  assert.match(source, /url\.username \|\| url\.password/);
  assert.match(source, /`https:\/\/\$\{candidate\}`/);
  assert.match(source, /window\.btoa\(binary\)/);
});

test("ships liquid glass and explicit light and dark themes", async () => {
  const css = await readFile(new URL("app/globals.css", root), "utf8");
  const page = await readFile(new URL("app/page.tsx", root), "utf8");
  assert.match(css, /backdrop-filter:\s*blur/);
  assert.match(css, /:root\[data-theme="dark"\]/);
  assert.match(css, /prefers-reduced-motion/);
  assert.match(page, /localStorage\.setItem\("owu-theme"/);
  assert.match(page, /aria-label="Toggle color theme"/);
});
