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

// A web address has a scheme, is localhost, is an IP, or is a dotted
// hostname. Anything else (plain words, Chinese text, phrases) is a search
// term and goes to Yandex instead of being mangled into a fake hostname.
function isWebAddress(value: string): boolean {
  const candidate = value.trim();
  if (!candidate) return false;
  if (/^[a-z][a-z\d+.-]*:\/\//i.test(candidate)) return true;
  if (/\s/.test(candidate)) return false;
  if (/^localhost(:\d+)?(\/.*)?$/i.test(candidate)) return true;
  if (/^(\d{1,3}\.){3}\d{1,3}(:\d{1,5})?(\/.*)?$/.test(candidate)) return true;
  return /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:\d{1,5})?(\/.*)?$/i.test(candidate);
}

function proxyAddress(value: string): { url?: string; error?: string; search?: boolean } {
  const input = value.trim();
  if (!input) return { error: "Enter a website address or a search term." };

  if (isWebAddress(input)) {
    const normalized = normalizeWebsite(input);
    if (!normalized.url) return normalized;
    const target = new URL(normalized.url);
    return {
      url: `/browse/${encodeOrigin(target.origin)}${target.pathname}${target.search}${target.hash}`,
    };
  }

  const query = new URLSearchParams({ text: input }).toString();
  return {
    search: true,
    url: `/browse/${encodeOrigin("https://yandex.com")}/search/?${query}`,
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

    setMessage(result.search ? "Searching Yandex…" : "Opening website…");
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
          <a
            className="github-link"
            href="https://github.com/TypeThe0ry/OWU"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="OWU GitHub repository"
          >
            <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">
              <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
            </svg>
            <span>GitHub</span>
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
          Enter any HTTP or HTTPS address, or just a search term. OWU opens websites through your private server and searches Yandex for everything else.
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
