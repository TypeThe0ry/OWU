const POLICY_DECISIONS = new Set([
  "allowed",
  "resource_not_authorized",
  "blocked",
  "port_not_allowed",
  "rate_limited",
]);

export function normalizeInputUrl(value) {
  const candidate = value.trim();
  if (!candidate) return { ok: false, reason: "empty" };

  const normalized = /^[a-z][a-z\d+.-]*:\/\//i.test(candidate)
    ? candidate
    : `https://${candidate}`;
  let url;
  try {
    url = new URL(normalized);
  } catch {
    return { ok: false, reason: "invalid" };
  }

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return { ok: false, reason: "unsupported_scheme" };
  }
  if (!url.hostname) return { ok: false, reason: "invalid" };
  if (url.username || url.password) {
    return { ok: false, reason: "embedded_credentials" };
  }

  return {
    ok: true,
    inputUrl: url.href,
    preview: {
      host: url.hostname,
      port: url.port || (url.protocol === "https:" ? "443" : "80"),
      protocol: url.protocol.slice(0, -1).toUpperCase(),
    },
  };
}

function fallbackDecision(status) {
  if (status === 401) return "resource_not_authorized";
  if (status === 403) return "resource_not_authorized";
  if (status === 429) return "rate_limited";
  return "service_error";
}

export async function checkAccess({ inputUrl, fetchImpl = fetch }) {
  let response;
  try {
    response = await fetchImpl("/api/access/check", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ input_url: inputUrl }),
    });
  } catch {
    return { decision: "service_error" };
  }

  let payload = {};
  try {
    payload = await response.json();
  } catch {
    // Never expose unstructured upstream text or stack traces.
  }

  const decision = POLICY_DECISIONS.has(payload.decision)
    ? payload.decision
    : fallbackDecision(response.status);
  if (decision === "allowed") {
    if (!response.ok || typeof payload.launch_url !== "string") {
      return { decision: "service_error" };
    }
    return { decision, launchUrl: payload.launch_url };
  }

  const retryHeader = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
  return {
    decision,
    retryAfterSeconds:
      decision === "rate_limited" && Number.isFinite(retryHeader)
        ? Math.min(Math.max(retryHeader, 1), 300)
        : decision === "rate_limited" ? 30 : undefined,
  };
}

export function safeLaunchUrl(value, currentOrigin) {
  try {
    const url = new URL(value, currentOrigin);
    if (url.username || url.password) return null;
    if (!url.pathname.startsWith("/_launch/")) return null;
    if (url.protocol === "https:") return url.href;
    if (url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) {
      return url.href;
    }
  } catch {
    // Invalid launch destinations fail closed.
  }
  return null;
}
