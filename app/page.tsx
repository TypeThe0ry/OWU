"use client";

import { FormEvent, useMemo, useState } from "react";

type ParsedTarget = {
  host: string;
  port: string;
  protocol: string;
};

const EXAMPLES = [
  "https://lab.example.org:8443",
  "https://docs.example.org",
  "http://devbox.example.net:3000",
];

function parseTarget(value: string): ParsedTarget | null {
  const candidate = value.trim();
  if (!candidate) return null;

  try {
    const normalized = /^https?:\/\//i.test(candidate)
      ? candidate
      : `https://${candidate}`;
    const url = new URL(normalized);

    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    if (!url.hostname || url.username || url.password) return null;

    return {
      host: url.hostname,
      port: url.port || (url.protocol === "https:" ? "443" : "80"),
      protocol: url.protocol.replace(":", "").toUpperCase(),
    };
  } catch {
    return null;
  }
}

export default function Home() {
  const [target, setTarget] = useState("");
  const [permissionConfirmed, setPermissionConfirmed] = useState(false);
  const [message, setMessage] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const parsed = useMemo(() => parseTarget(target), [target]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!parsed) {
      setMessage("Enter a valid HTTP or HTTPS address, with an optional port.");
      return;
    }

    if (!permissionConfirmed) {
      setMessage("Confirm that you own this resource or have explicit permission to access it.");
      return;
    }

    setMessage(
      `${parsed.host}:${parsed.port} is ready for an authorization check. Sign in to open a secure session.`,
    );
  }

  return (
    <>
      <a className="skip-link" href="#main-content">Skip to main content</a>
      <main id="main-content">
      <header className="site-header">
        <a className="brand" href="#top" aria-label="Permit home">
          <span className="brand-mark" aria-hidden="true">P</span>
          <span>Permit</span>
        </a>

        <button
          className="menu-button"
          type="button"
          aria-label="Toggle navigation"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((open) => !open)}
        >
          Menu
        </button>

        <nav className={menuOpen ? "nav-links nav-open" : "nav-links"} aria-label="Main navigation">
          <a href="#how-it-works">How it works</a>
          <a href="#mac">Mac app</a>
          <a href="#safety">Safety</a>
          <button className="text-button" type="button">Sign in</button>
        </nav>
      </header>

      <section className="hero" id="top">
        <div className="eyebrow"><span /> Free public beta</div>
        <h1>One address.<br />Your authorized network.</h1>
        <p className="hero-copy">
          Open websites you own or are allowed to use—from any browser, with no subscription.
          Paste an address, include a port when you need one, and Permit handles the secure route.
        </p>

        <div className="access-panel">
          <form className="access-form" onSubmit={handleSubmit} noValidate>
            <label htmlFor="target-url">Website address</label>
            <div className="input-row">
              <div className="url-field">
                <span className="connection-dot" aria-hidden="true" />
                <input
                  id="target-url"
                  inputMode="url"
                  type="text"
                  value={target}
                  onChange={(event) => {
                    setTarget(event.target.value);
                    setMessage("");
                  }}
                  placeholder="https://lab.example.org:8443"
                  aria-describedby="address-help form-message"
                  autoComplete="url"
                />
              </div>
              <button className="primary-button" type="submit">Open securely <span aria-hidden="true">→</span></button>
            </div>
            <label className="permission-check">
              <input
                type="checkbox"
                checked={permissionConfirmed}
                onChange={(event) => {
                  setPermissionConfirmed(event.target.checked);
                  setMessage("");
                }}
              />
              <span>I confirm that I own this resource or have explicit permission to access it.</span>
            </label>
            <div className="form-footer">
              <p id="address-help">HTTP and HTTPS · Custom ports supported</p>
              <p className={message && !parsed ? "form-message form-error" : "form-message"} id="form-message" aria-live="polite">
                {message}
              </p>
            </div>
          </form>

          <div className="route-preview" aria-label="Parsed route preview">
            <span className="route-label">Secure route</span>
            <span><small>PROTOCOL</small>{parsed?.protocol ?? "HTTPS"}</span>
            <span><small>HOST</small>{parsed?.host ?? "lab.example.org"}</span>
            <span><small>PORT</small>{parsed?.port ?? "8443"}</span>
            <span className="status-pill"><i /> Authorization required</span>
          </div>
        </div>

        <div className="trust-line" aria-label="Service commitments">
          <span>Free to use</span>
          <span>HTTPS on port 443</span>
          <span>No anonymous proxying</span>
          <span>Activity is auditable</span>
        </div>
      </section>

      <section className="steps-section" id="how-it-works">
        <div className="section-heading">
          <p>How it works</p>
          <h2>Less setup.<br />More access.</h2>
        </div>
        <div className="steps-grid">
          <article>
            <span>01</span>
            <h3>Paste a destination</h3>
            <p>Use a full URL or a domain with a port. Permit validates the destination before anything connects.</p>
          </article>
          <article>
            <span>02</span>
            <h3>Confirm permission</h3>
            <p>Sign in and match the resource policy set by its owner. Short sessions keep access focused and revocable.</p>
          </article>
          <article>
            <span>03</span>
            <h3>Open it securely</h3>
            <p>Traffic travels through a controlled HTTPS gateway. The destination port stays private from the public internet.</p>
          </article>
        </div>
      </section>

      <section className="mac-section" id="mac">
        <div className="mac-copy">
          <p className="section-kicker">Permit for macOS</p>
          <h2>Websites are just<br />the beginning.</h2>
          <p>
            The Mac app extends the same permission model to SSH, databases, local development tools,
            and private services—without changing how those apps work.
          </p>
          <div className="release-badge"><span>Coming next</span> Native SwiftUI app</div>
        </div>

        <div className="mac-window" aria-label="Preview of Permit for macOS">
          <div className="window-bar"><i /><i /><i /><span>Permit</span></div>
          <div className="window-body">
            <div className="app-icon">P</div>
            <p className="app-state">Connected</p>
            <h3>Lab workspace</h3>
            <p className="app-meta">Secure session · 24 min remaining</p>
            <div className="traffic-card">
              <div><span>ROUTE</span><strong>Authorized resources</strong></div>
              <div><span>GATEWAY</span><strong>Singapore · 18 ms</strong></div>
              <div><span>LOCAL PROXY</span><strong>127.0.0.1:1080</strong></div>
            </div>
            <button type="button">Disconnect</button>
          </div>
        </div>
      </section>

      <section className="safety-section" id="safety">
        <p className="section-kicker">Built for legitimate access</p>
        <div>
          <h2>Free should still<br />be responsible.</h2>
          <p>
            Permit is not an open proxy. Every session is tied to a person, a device, and an approved destination.
            Private ranges, metadata endpoints, and unapproved ports are denied by default.
          </p>
        </div>
        <ul>
          <li>Resource allowlists</li>
          <li>Short-lived sessions</li>
          <li>Per-device keys</li>
          <li>Rate and usage limits</li>
        </ul>
      </section>

      <section className="try-section">
        <p>Ready when your resource is.</p>
        <h2>Start with an address.</h2>
        <div className="example-links" aria-label="Example addresses">
          {EXAMPLES.map((example) => (
            <button key={example} type="button" onClick={() => {
              setTarget(example);
              setPermissionConfirmed(false);
              setMessage("");
              document.getElementById("top")?.scrollIntoView({ behavior: "smooth" });
            }}>{example}</button>
          ))}
        </div>
      </section>

      <footer>
        <a className="brand footer-brand" href="#top"><span className="brand-mark" aria-hidden="true">P</span><span>Permit</span></a>
        <p>Authorized access, open to everyone.</p>
        <div><a href="#safety">Acceptable use</a><a href="#safety">Privacy</a><span>© 2026 Permit</span></div>
      </footer>
      </main>
    </>
  );
}
