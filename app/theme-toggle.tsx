"use client";

export function toggleTheme() {
  const current = document.documentElement.dataset.theme
    ?? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const next = current === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = next;
  window.localStorage.setItem("owu-theme", next);
}

export default function ThemeToggle() {
  return (
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
  );
}
