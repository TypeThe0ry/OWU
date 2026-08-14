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

const numberFormat = new Intl.NumberFormat("zh-CN");

function formatTime(value?: string): string {
  if (!value) return "–";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "–" : date.toLocaleString("zh-CN", { hour12: false });
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
            <h1 id="stats-title">使用统计</h1>
            <span className="stats-updated" aria-live="polite">
              {failed ? "加载失败，稍后重试" : data ? `更新于 ${formatTime(data.updatedAt)}` : "加载中…"}
            </span>
          </div>

          {!enabled ? (
            <p className="stats-empty">统计未启用：{data?.message ?? "请联系管理员配置。"}</p>
          ) : (
            <>
              <div className="stats-grid">
                <section className="stat-card">
                  <div className="stat-label">访问人数</div>
                  <div className="stat-value">{numberFormat.format(data?.visitorsTotal ?? 0)}</div>
                  <div className="stat-sub">今日 {numberFormat.format(data?.visitorsToday ?? 0)} 人 · 匿名累计</div>
                </section>
                <section className="stat-card">
                  <div className="stat-label">使用次数</div>
                  <div className="stat-value">{numberFormat.format(data?.usesTotal ?? 0)}</div>
                  <div className="stat-sub">今日 {numberFormat.format(data?.usesToday ?? 0)} 次</div>
                </section>
                <section className="stat-card">
                  <div className="stat-label">统计起始</div>
                  <div className="stat-value stat-value-date">{formatTime(data?.since)}</div>
                  <div className="stat-sub">每 10 秒自动刷新</div>
                </section>
              </div>

              <h2 className="stats-section-title">用户最常访问的网站</h2>
              {topSites.length === 0 ? (
                <p className="stats-empty">暂无数据</p>
              ) : (
                <ol className="stats-sites">
                  {topSites.map((entry, index) => (
                    <li key={entry.site}>
                      <span className="stats-rank">{index + 1}</span>
                      <span className="stats-site">{entry.site}</span>
                      <span className="stats-count">{numberFormat.format(entry.uses)} 次</span>
                    </li>
                  ))}
                </ol>
              )}
            </>
          )}

          <p className="stats-note">
            统计为匿名方式：仅以不可逆哈希标识访问者，不保存 IP 等可识别信息；目标网站仅记录域名与使用次数。
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
