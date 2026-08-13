"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { checkAccess, normalizeInputUrl, safeLaunchUrl } from "./access-check.mjs";

const VALIDATION_MESSAGES: Record<string, string> = {
  empty: "Enter a website address.",
  invalid: "Enter a valid address, such as https://example.com:8443.",
  unsupported_scheme: "Permit supports HTTP and HTTPS websites only.",
  embedded_credentials: "Remove the username or password from the address and try again.",
};

const DECISION_MESSAGES: Record<string, string> = {
  resource_not_authorized: "This resource has not been approved for public access.",
  blocked: "This destination can’t be opened through Permit.",
  port_not_allowed: "This port isn’t approved for this resource.",
  rate_limited: "Too many attempts. Wait a moment, then try again.",
  service_error: "Permit couldn’t open this resource right now. Try again later.",
};

type Phase = "idle" | "checking" | "opening";

export default function Home() {
  const [target, setTarget] = useState("");
  const [message, setMessage] = useState("");
  const [phase, setPhase] = useState<Phase>("idle");
  const [retryAfter, setRetryAfter] = useState(0);
  const messageRef = useRef<HTMLParagraphElement>(null);
  const normalized = useMemo(() => normalizeInputUrl(target), [target]);
  const busy = phase !== "idle";

  useEffect(() => {
    if (retryAfter <= 0) return;
    const timer = window.setTimeout(() => {
      setRetryAfter((seconds) => Math.max(0, seconds - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [retryAfter]);

  function showError(nextMessage: string) {
    setMessage(nextMessage);
    window.requestAnimationFrame(() => messageRef.current?.focus());
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || retryAfter > 0) return;

    if (!normalized.ok) {
      showError(VALIDATION_MESSAGES[normalized.reason] ?? VALIDATION_MESSAGES.invalid);
      return;
    }

    setPhase("checking");
    setMessage("Checking whether this resource is available…");
    const result = await checkAccess({ inputUrl: normalized.inputUrl });

    if (result.decision === "allowed") {
      const launchUrl = safeLaunchUrl(result.launchUrl, window.location.origin);
      if (launchUrl) {
        setPhase("opening");
        setMessage("Resource approved. Opening securely…");
        window.location.assign(launchUrl);
        return;
      }
      setPhase("idle");
      showError(DECISION_MESSAGES.service_error);
      return;
    }

    setPhase("idle");
    if (result.decision === "rate_limited") {
      setRetryAfter(result.retryAfterSeconds ?? 30);
    }
    showError(DECISION_MESSAGES[result.decision] ?? DECISION_MESSAGES.service_error);
  }

  return (
    <main className="permit-shell">
      <section className="permit-card" aria-labelledby="page-title">
        <div className="brand" aria-label="Permit">
          <span className="brand-mark" aria-hidden="true">P</span>
          <span>Permit</span>
        </div>

        <div className="intro">
          <p className="eyebrow">AUTHORIZED WEB ACCESS</p>
          <h1 id="page-title">Open a public resource.</h1>
          <p>Enter the address of a resource that has been registered with Permit.</p>
        </div>

        <form className="access-form" onSubmit={handleSubmit} noValidate>
          <label htmlFor="target-url">Website address</label>
          <div className="input-row">
            <input
              id="target-url"
              inputMode="url"
              type="text"
              maxLength={2048}
              value={target}
              onChange={(event) => {
                setTarget(event.target.value);
                setMessage("");
              }}
              placeholder="https://service.example.com:8443"
              aria-describedby="address-help form-message"
              aria-invalid={Boolean(message && phase === "idle")}
              autoComplete="url"
              disabled={busy}
            />
            <button type="submit" disabled={busy || retryAfter > 0}>
              {phase === "checking"
                ? "Checking…"
                : phase === "opening"
                  ? "Opening…"
                  : retryAfter > 0
                    ? `Try again in ${retryAfter}s`
                    : "Visit website"}
            </button>
          </div>
          <p id="address-help" className="helper">HTTP and HTTPS addresses only. Custom ports are supported.</p>
          <p
            ref={messageRef}
            id="form-message"
            className={message && phase === "idle" ? "status status-error" : "status"}
            aria-live="polite"
            aria-busy={busy}
            tabIndex={-1}
          >
            {message}
          </p>
        </form>

        <p className="boundary-note">Only pre-registered public resources can be opened. Permit is not an open proxy.</p>
      </section>
    </main>
  );
}
