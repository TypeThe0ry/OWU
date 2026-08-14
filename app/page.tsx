"use client";

import { FormEvent, useEffect, useState } from "react";
import ThemeToggle from "./theme-toggle";

interface TopSite {
  site: string;
  uses: number;
}

function normalizeWebsite(value: string): { url?: string; error?: string } {
  const candidate = value.trim();
  if (!candidate) return { error: "Enter a website address." };

  const normalized = /^[a-z][a-zd+.-]*:///i.test(candidate)
    ? candidate
    : `https://${candidate}`;

  try {
    const url = new URL(normalized);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return { error: "OWU opens HTTP and HTTPS websites only." };
    }
    if (!url.hostname) return { error: "Enter a valid website address." };
    if (url.username || url.password) {
      return { error: "Remove the username or password from the address." };
    }
    return { url: url.href };
  } catch {
    return { error: "Enter a valid address, such as https://example.com." };
  }
}

function encodeOrigin(origin: string): string {
  const bytes = new TextEncoder().encode(origin);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return window.btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function proxyAddress(value: string): { url?: string; error?: string } {
  const normalized = normalizeWebsite(value);
  if (!normalized.url) return normalized;
  const target = new URL(normalized.url);
  return {
    url: `/browse/${encodeOrigin(target.origin)}${target.pathname}${target.search}${target.hash}`,
  };
}

export default function Home() {
  const [target, setTarget] = useState("");
  const [message, setMessage] = useState("");
  const [topSites, setTopSites] = useState<TopSite[] | null>(null);

  useEffect(() => {
    let active = true;
    fetch("/stats/api", { cache: "no-store" })
      .then((response) => response.json())
      .then((data) => {
        if (active && data.enabled && Array.isArray(data.topSites)) {
          setTopSites(data.topSites.slice(0, 6));
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = proxyAddress(target);
    if (!result.url) {
      setMessage(result.error ?? "Enter a valid website address.");
      return;
    }

    setMessage("Opening website…");
    window.location.assign(result.url);
  }

  return (
    <main className="owu-shell">
      <div className="ambient ambient-one" aria-hidden="true" />
      <div className="ambient ambient-two" aria-hidden="true" />
      <div className="ambient ambient-three" aria-hidden="true" />

      <header className="topbar">
        <div className="wordmark" aria-label="Open Website Unblocker">
          <span className="wordmark-icon" aria-hidden="true">
            <i />
          </span>
          <span className="wordmark-copy">
            <strong>OWU</strong>
            <small>Open Website Unblocker</small>
          </span>
        </div>

        <div className="topbar-actions">
          <a className="stats-link" href="/stats" aria-label="View usage statistics">
            <i aria-hidden="true">▦</i>
            <span>Stats</span>
          </a>
          <ThemeToggle />
        </div>
      </header>

      <section className="hero" aria-labelledby="page-title">
        <div className="hero-badge"><span /> Personal web proxy</div>
        <h1 id="page-title">
          Open the web.
          <span>One address away.</span>
        </h1>
        <p className="hero-copy">
          Enter any HTTP or HTTPS address. OWU loads it through your private server while the address bar stays on OWU.
        </p>

        <form className="glass-search" onSubmit={handleSubmit} noValidate>
          <label htmlFor="website-url">Website address</label>
          <div className="search-row">
            <span className="search-icon" aria-hidden="true">⌕</span>
            <input
              id="website-url"
              type="text"
              inputMode="url"
              autoComplete="url"
              autoCapitalize="none"
              spellCheck={false}
              maxLength={2048}
              value={target}
              onChange={(event) => {
                setTarget(event.target.value);
                setMessage("");
              }}
              placeholder="example.com"
              aria-describedby="form-message direct-note"
              aria-invalid={Boolean(message && message !== "Opening website…")}
            />
            <button type="submit">
              <span>Open website</span>
              <i aria-hidden="true">↗</i>
            </button>
          </div>
          <p
            id="form-message"
            className={message && message !== "Opening website…" ? "form-message error" : "form-message"}
            aria-live="polite"
          >
            {message}
          </p>
        </form>

        {topSites && topSites.length > 0 && (
          <section className="popular-sites" aria-label="Popular websites">
            <span className="popular-label">Everyone is browsing</span>
            <div className="popular-chips">
              {topSites.map((entry) => (
                <span className="popular-chip" key={entry.site}>
                  <span className="popular-host">{entry.site}</span>
                  <span className="popular-count">{entry.uses}</span>
                </span>
              ))}
            </div>
          </section>
        )}

        <p className="direct-note" id="direct-note">
          <span aria-hidden="true">◇</span>
          Browser-password protected. Destination sites see the OWU server connection, not your device address.
        </p>
      </section>

      <footer>
        <span>OWU</span>
        <span>Simple by design.</span>
      </footer>
    </main>
  );
}
