"use client";

import { useEffect, useState } from "react";
import ThemeToggle from "../theme-toggle";

interface TopSite {
  site: string;
  uses: number;
}

interface StatsData {
  enabled: boolean;
  message?: string;
  since?: string;
  updatedAt?: string;
  visitorsTotal?: number;
  visitorsToday?: number;
  usesTotal?: number;
  usesToday?: number;
  topSites?: TopSite[];
}

const numberFormat = new Intl.NumberFormat("en-US");

function formatTime(value?: string): string {
  if (!value) return "–";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "–" : date.toLocaleString("en-US", { hour12: false });
}

export default function StatsPage() {
  const [data, setData] = useState<StatsData | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    async function refresh() {
      try {
        const response = await fetch("/stats/api", { cache: "no-store" });
        const json = (await response.json()) as StatsData;
        if (active) {
          setData(json);
          setFailed(false);
        }
      } catch {
        if (active) setFailed(true);
      }
    }
    refresh();
    const timer = setInterval(refresh, 10000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, []);

  const enabled = data?.enabled === true;
  const topSites = enabled && data?.topSites ? data.topSites : [];

  return (
    <main className="owu-shell stats-shell">
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
            <small>Usage Statistics</small>
          </span>
        </div>

        <div className="topbar-actions">
          <a className="stats-link stats-link-back" href="/" aria-label="Back to home">
            <i aria-hidden="true">←</i>
            <span>Home</span>
          </a>
          <ThemeToggle />
        </div>
      </header>

      <section className="stats-stage" aria-labelledby="stats-title">
        <div className="stats-card">
          <div className="stats-head">
            <h1 id="stats-title">Usage Statistics</h1>
            <span className="stats-updated" aria-live="polite">
              {failed ? "Failed to load — retrying" : data ? `Updated ${formatTime(data.updatedAt)}` : "Loading…"}
            </span>
          </div>

          {!enabled ? (
            <p className="stats-empty">Statistics are not enabled: {data?.message ?? "Contact the operator to enable them."}</p>
          ) : (
            <>
              <div className="stats-grid">
                <section className="stat-card">
                  <div className="stat-label">Visitors</div>
                  <div className="stat-value">{numberFormat.format(data?.visitorsTotal ?? 0)}</div>
                  <div className="stat-sub">{numberFormat.format(data?.visitorsToday ?? 0)} today · anonymous total</div>
                </section>
                <section className="stat-card">
                  <div className="stat-label">Uses</div>
                  <div className="stat-value">{numberFormat.format(data?.usesTotal ?? 0)}</div>
                  <div className="stat-sub">{numberFormat.format(data?.usesToday ?? 0)} today</div>
                </section>
                <section className="stat-card">
                  <div className="stat-label">Counting since</div>
                  <div className="stat-value stat-value-date">{formatTime(data?.since)}</div>
                  <div className="stat-sub">Auto-refreshes every 10 seconds</div>
                </section>
              </div>

              <h2 className="stats-section-title">Most visited websites</h2>
              {topSites.length === 0 ? (
                <p className="stats-empty">No data yet</p>
              ) : (
                <ol className="stats-sites">
                  {topSites.map((entry, index) => (
                    <li key={entry.site}>
                      <span className="stats-rank">{index + 1}</span>
                      <span className="stats-site">{entry.site}</span>
                      <span className="stats-count">{numberFormat.format(entry.uses)} uses</span>
                    </li>
                  ))}
                </ol>
              )}
            </>
          )}

          <p className="stats-note">
            Statistics are anonymous: visitors are identified only by an irreversible hash and no IP or other identifying
            data is stored; destination websites are recorded by domain and usage count only.
          </p>
        </div>
      </section>

      <footer>
        <span>OWU</span>
        <span>Simple by design.</span>
      </footer>
    </main>
  );
}
