"use client";

import { FormEvent, useState } from "react";

function normalizeWebsite(value: string): { url?: string; error?: string } {
  const candidate = value.trim();
  if (!candidate) return { error: "Enter a website address." };

  const normalized = /^[a-z][a-z\d+.-]*:\/\//i.test(candidate)
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

export default function Home() {
  const [target, setTarget] = useState("");
  const [message, setMessage] = useState("");

  function toggleTheme() {
    const current = document.documentElement.dataset.theme
      ?? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    window.localStorage.setItem("owu-theme", next);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = normalizeWebsite(target);
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

        <button
          className="theme-toggle"
          type="button"
          onClick={toggleTheme}
          aria-label="Toggle color theme"
        >
          <span className="theme-track" aria-hidden="true">
            <span className="theme-thumb">
              <span className="theme-icon-light">☀</span>
              <span className="theme-icon-dark">☾</span>
            </span>
          </span>
          <span className="theme-label" aria-hidden="true">
            <span className="theme-label-light">Light</span>
            <span className="theme-label-dark">Dark</span>
          </span>
        </button>
      </header>

      <section className="hero" aria-labelledby="page-title">
        <div className="hero-badge"><span /> Web address launcher</div>
        <h1 id="page-title">
          Open the web.
          <span>One address away.</span>
        </h1>
        <p className="hero-copy">
          Enter any HTTP or HTTPS address. OWU opens it directly in your browser—no account and no setup.
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

        <p className="direct-note" id="direct-note">
          <span aria-hidden="true">◇</span>
          Direct browser navigation. OWU does not relay traffic or bypass network controls.
        </p>
      </section>

      <footer>
        <span>OWU</span>
        <span>Simple by design.</span>
      </footer>
    </main>
  );
}
