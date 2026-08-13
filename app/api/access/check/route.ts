const SAFE_RESPONSE_FIELDS = [
  "decision",
  "message",
  "normalized_url",
  "launch_url",
  "expires_at",
] as const;

export async function POST(request: Request) {
  const controlApiUrl = process.env.CONTROL_API_URL?.replace(/\/$/, "");
  if (!controlApiUrl) {
    return Response.json({ decision: "service_error" }, { status: 503 });
  }

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return Response.json({ decision: "blocked" }, { status: 400 });
  }
  if (
    typeof body !== "object" || body === null ||
    typeof (body as { input_url?: unknown }).input_url !== "string" ||
    (body as { input_url: string }).input_url.length > 2048
  ) {
    return Response.json({ decision: "blocked" }, { status: 400 });
  }

  let upstream: Response;
  try {
    upstream = await fetch(`${controlApiUrl}/v1/access/check`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ input_url: (body as { input_url: string }).input_url }),
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return Response.json({ decision: "service_error" }, { status: 503 });
  }

  let source: Record<string, unknown> = {};
  try {
    source = await upstream.json() as Record<string, unknown>;
  } catch {
    return Response.json({ decision: "service_error" }, { status: 502 });
  }
  const safePayload: Record<string, unknown> = {};
  for (const field of SAFE_RESPONSE_FIELDS) {
    if (typeof source[field] === "string") safePayload[field] = source[field];
  }

  const responseHeaders = new Headers({ "Cache-Control": "no-store" });
  const retryAfter = upstream.headers.get("retry-after");
  if (retryAfter) responseHeaders.set("Retry-After", retryAfter);
  return Response.json(safePayload, { status: upstream.status, headers: responseHeaders });
}
