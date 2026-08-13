# Web Product UI/UX and Copy Specification

## 1. Product position

**Recommended working name:** PermitLink  
**Tagline:** Open what you're allowed to reach.

Alternatives: **Gatepass**, **ClearRoute**, and **AccessPort**. Complete domain and trademark checks before choosing a public name.

PermitLink is a free access service for opening HTTP/HTTPS resources that the user owns or has explicit permission to use. Anyone may create a free account, but no connection is anonymous and user attestation alone never makes a target eligible.

**Product promise:** Paste an approved web address, including a custom port, and open it through a secure, policy-checked connection.

**Do not describe it as:** an unblocker, anonymous proxy, censorship bypass, firewall bypass, or a way to visit any site.

## 2. Product rules that shape the UI

- Free account creation is open to everyone; payment and upgrade prompts are absent from the MVP.
- Sign-in is required before any upstream connection is created.
- The user must confirm ownership or permission, but the service must also verify access through an existing resource grant, owner invitation, DNS/`/.well-known/` proof, or an approved private connector.
- The web product supports only `http://` and `https://`. SSH, databases, arbitrary TCP, and private DNS belong in the macOS app.
- Custom ports are allowed only when the resource policy allows them. They are never exposed as public listening ports.
- Credentials in URLs, such as `https://user:pass@example.com`, are rejected.
- Loopback, link-local, cloud metadata, and unapproved private-network destinations are blocked before connection. DNS must be checked again at connect time to prevent rebinding.

## 3. Information architecture

### Signed-out home

1. Compact header: PermitLink logo/name, **How it works**, **Safety**, **Sign in**.
2. Hero and URL form: the entire primary task fits above the fold.
3. Three-step explanation: **Paste**, **Verify**, **Open**.
4. Safety statement: what can and cannot be accessed.
5. Footer: **Documentation**, **Acceptable Use**, **Privacy**, **Security**, **Status**.

### Signed-in home

Keep the same URL form as the visual focus. Add only:

- **Approved resources**: verified or invited destinations, with an **Open** action.
- **Recent**: at most five recent destinations; never show sensitive paths or query strings by default.
- Account menu: **Devices**, **Resources**, **Activity**, **Sign out**.

Do not turn the MVP home into an admin dashboard. Resource and audit management should live on separate routes.

### Supporting routes

| Route | Purpose |
|---|---|
| `/` | Enter and open an approved URL |
| `/sign-in` | Passwordless or OIDC sign-in |
| `/resources` | Verify, accept invitations, and manage permitted targets |
| `/activity` | Review access time, device, target host/port, and result |
| `/how-it-works` | Plain-language setup and authorization model |
| `/safety` | Allowed use, blocked destinations, reporting, and privacy |

## 4. First-screen layout and final English copy

### Header

- Brand: **PermitLink**
- Links: **How it works** · **Safety**
- Signed out action: **Sign in**
- Signed in action: avatar/account menu

### Hero

**Eyebrow:** `FREE AUTHORIZED ACCESS`

**H1:** `Open your approved web resources.`

**Supporting text:**  
`Enter a website you own or have permission to use. Custom ports are supported.`

**Field label:** `Website address`

**Placeholder:** `https://service.example.com:8443`

**Persistent helper text:**  
`HTTP and HTTPS only. We’ll use HTTPS when no scheme is entered.`

**Required confirmation:**  
`I confirm that I own this resource or have explicit permission to access it.`

**Primary button:** `Open securely`

**Trust note below the button:**  
`Free to use. Sign-in and authorization checks are required. PermitLink is not an anonymous or open proxy.`

### Three-step explanation

**Heading:** `Simple access, clear permission.`

1. **Paste the address** — `Use a hostname, full URL, or approved custom port.`
2. **Confirm access** — `Sign in and use an existing grant, owner invite, or resource verification.`
3. **Open securely** — `PermitLink checks policy before creating a short-lived connection.`

### Safety block

**Heading:** `Built for resources you’re allowed to use.`

**Body:**  
`PermitLink blocks anonymous access, unapproved destinations, unsafe network ranges, and disallowed ports. Access is logged without recording passwords or page contents.`

**Link:** `Read the safety policy`

## 5. Input behavior

### Parsing and normalization

- Trim leading and trailing spaces.
- A bare host such as `service.example.com` becomes `https://service.example.com/`.
- Preserve an explicit port, path, query, and fragment. Do not expose path/query values in recents or audit summaries.
- Convert the hostname to lowercase and IDNs to their canonical ASCII form for policy checks; display a safe human-readable form.
- Never send a network request until syntax, identity, authorization, DNS, IP range, scheme, and port checks pass.
- Pressing Enter submits the form. Pasting a URL does not auto-submit.
- Keep the entered value on recoverable errors so the user can edit it.

### Progressive authorization

After the user selects **Open securely**:

1. If signed out, preserve the URL locally and show sign-in. Resume validation after successful sign-in.
2. If the target matches an existing grant, continue immediately.
3. If the user can verify the target, offer **Verify this resource**.
4. If another owner controls it, offer **Request access** or **Use an invite**.
5. If the target is categorically unsafe or disallowed, stop; do not offer a bypass.

The confirmation checkbox is an explicit user attestation, not the authorization mechanism. Remember it only for the current signed-in session and ask again when the target host changes.

### Loading and success

- Button while checking: `Checking access…`
- Button while connecting: `Opening…`
- Status text: `Verifying your permission and the destination.`
- On success, navigate in the same tab to the resource-specific gateway origin. Avoid pop-ups.
- If navigation takes over two seconds, show **Cancel** and keep the URL visible.

## 6. Validation and error copy

Errors appear directly below the field, receive focus on submit, and are also announced by an `aria-live="polite"` region. Never reveal internal IPs, policy identifiers, or upstream stack traces.

| Condition | Message | Action |
|---|---|---|
| Empty | `Enter a website address.` | Focus the field |
| Invalid syntax | `Enter a valid address, such as https://example.com:8443.` | None |
| Unsupported scheme | `PermitLink supports HTTP and HTTPS websites only.` | `Learn about the macOS app` |
| Embedded credentials | `Remove the username or password from the address and try again.` | None |
| Authorization unchecked | `Confirm that you’re allowed to access this resource.` | Focus the checkbox |
| Sign-in required | `Sign in to check your access to this resource.` | `Sign in` |
| No grant | `We couldn’t confirm that you’re allowed to access this resource.` | `Verify this resource` / `Request access` |
| Port not allowed | `Port {port} isn’t approved for this resource.` | `View approved ports` |
| Blocked destination | `This destination can’t be opened through PermitLink.` | `Why destinations are blocked` |
| Plain HTTP | `This site does not use HTTPS. Your connection from PermitLink to the site may not be encrypted.` | `Go back` / `Continue if approved` |
| TLS failure | `We couldn’t verify this site’s security certificate.` | `Try again` / `Contact the resource owner` |
| Unreachable/timeout | `The resource didn’t respond. Check the address or try again later.` | `Try again` |
| Rate limit | `Too many attempts. Wait a moment, then try again.` | Disabled retry with countdown |
| Session expired | `Your session expired. Sign in to continue.` | `Sign in` |
| Service fault | `PermitLink couldn’t open this resource right now.` | `Try again` / `View status` |

Do not use playful language for security or permission failures. Do not say **blocked by your school/company/region**, because PermitLink does not diagnose or help evade third-party network policy.

## 7. Visual and interaction direction

- Calm, utility-first, and trustworthy rather than “hacker” or VPN-themed.
- One centered content column, maximum form width about 680 px.
- Use a neutral background, high-contrast text, one restrained accent color, and a distinct non-color error cue.
- The URL input and primary button are the strongest visual elements. Avoid carousels, testimonials, pricing tables, download banners, animated maps, and protocol jargon above the fold.
- Use a real shield/check icon from the project’s icon library only where it adds meaning; never use padlock decoration as proof of security.
- Keep security copy visible rather than hiding it behind a tooltip.

## 8. Mobile acceptance

At 320–430 px widths:

- No horizontal scrolling at 200% browser zoom.
- The field, checkbox, and primary button stack in one column; the button is full width.
- Interactive targets are at least 44 by 44 CSS pixels.
- The keyboard uses the URL input mode and does not hide the active error or primary action.
- Long hostnames and IPv6 literals wrap or truncate safely without moving actions off-screen.
- Sign-in returns to the preserved URL and form state.
- The full primary task, helper text, and trust note appear before explanatory marketing sections.

## 9. Accessibility acceptance

Target WCAG 2.2 AA:

- Every input has a persistent visible label; placeholder text is never the only label.
- Keyboard order follows the visual order, with a visible focus indicator on every control.
- Text contrast is at least 4.5:1; large text and meaningful UI graphics are at least 3:1.
- Errors are linked to the field with `aria-describedby`, summarized after submit, and never communicated by color alone.
- Loading indicators expose a text status; motion honors `prefers-reduced-motion`.
- The confirmation uses a native checkbox with the entire sentence as its clickable label.
- Headings are hierarchical, landmarks are named, and a **Skip to main content** link is the first focusable element.
- Browser zoom to 200% and text spacing overrides do not hide or overlap content.
- Automated checks have no critical violations, and the complete open-resource flow is manually operable with keyboard and VoiceOver.

## 10. MVP analytics and content QA

Track only the minimum events needed to improve the funnel: form submitted, parse failed, sign-in started/completed, authorization result category, connection started/succeeded/failed, and time to open. Never place full URLs, paths, query strings, credentials, page content, or raw internal addresses in analytics.

Before release, verify:

- All UI is English and uses the same terms: **resource**, **approved**, **permission**, and **open**.
- **Free** never implies anonymous or unrestricted use.
- A user cannot trigger an upstream request by changing client-side state or skipping the checkbox.
- All error and loading states above are represented in the interface and test fixtures.
- Legal and safety links are reachable before sign-in.
