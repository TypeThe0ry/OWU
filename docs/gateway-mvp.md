# Authorized Access Gateway MVP

> 状态：可执行架构草案  
> 范围：用户访问其自有服务器、实验室或明确获准的 Web 资源  
> 非目标：匿名开放代理、任意目标转发、绕过组织或地区网络策略、流量伪装、TLS 中间人

## 1. 产品承诺与安全边界

面向用户的体验可以保持为一个英文 URL 输入框：

```text
Enter an authorized URL, including an optional port
[ https://lab.example.org:8443/dashboard                 ] [ Open securely ]
```

但“输入即可访问”不等于“任意 URL 都可由平台代为请求”。一次成功访问必须同时满足：

1. 用户已登录；免费表示不向终端用户收费，而不是允许匿名使用。
2. URL 对应一个已注册且已验证控制权的资源。
3. 当前用户或其用户组拥有该资源的访问权限。
4. 协议、端口和路径符合资源策略。
5. 连接时解析出的每个实际地址都通过 SSRF 和网络出口策略检查。

未匹配到资源时，产品应显示 `This resource has not been authorized yet`，并向可能的资源所有者提供验证流程，绝不能回退为直接抓取目标。

公开免费服务仍有真实的主机、带宽、邮件和防滥用成本。MVP 可以做到“用户免费”，但不能承诺“无限流量且运营零成本”。应以赞助预算、小额基础设施、严格配额和透明的公平使用规则启动。

## 2. MVP 范围

### 2.1 包含

- 英文 Web 门户与 URL 启动入口。
- 电子邮件验证或 OIDC 登录；无匿名代理模式。
- 每个资源一个固定 `http` 或 `https` origin，包括可选端口。
- DNS TXT 或 `/.well-known/authorized-access-challenge` 所有权验证。
- 同源 HTTP/1.1、HTTP/2 上游请求和 WebSocket 转发。
- `GET`、`HEAD`、`POST`、`PUT`、`PATCH`、`DELETE`、`OPTIONS`；拒绝 `CONNECT` 和 `TRACE`。
- 用户/组到资源的授权、路径前缀限制、会话 TTL、审计与公平使用配额。
- 独立资源子域隔离 Cookie，例如 `r-a8f31.access.example.com`。
- Docker Compose 单区域部署，可平滑拆分控制平面与数据平面。

### 2.2 不包含

- 任意 URL 的开放抓取或网页重写服务。
- HTTP `CONNECT`、SOCKS、原始 TCP、UDP；这些属于后续 macOS 客户端与专用隧道阶段。
- 跳过上游 TLS 证书校验、自定义不受控 CA 或 TLS 解密。
- 全量 HTML/JavaScript/CSP 重写。
- 跨任意 origin 自动跟随重定向。
- 多租户计费、无限免费流量和高可用多区域。

### 2.3 Web 兼容性边界

MVP 是按资源配置的反向代理，不是远程浏览器。相对链接、同源 API、常规表单和 WebSocket 应可工作。网关会安全改写同源 `Location`、`Origin`、`Referer` 以及必要的 Cookie 属性，但不改写 HTML 或 JavaScript 正文。

以下应用可能需要资源管理员设置外部 Base URL，或在后续版本使用专用连接器：

- 在 HTML/JS 中硬编码绝对 origin 的应用；
- 严格固定 CSP、OAuth 回调地址或 WebAuthn RP ID 的应用；
- 跨多个未登记 origin 加载或跳转的应用；
- 依赖客户端证书、非 HTTP 协议或浏览器直连私有 DNS 的应用。

## 3. 总体架构

```text
Browser
  | HTTPS :443
  v
Edge DNS / WAF / basic IP rate limit
  |-----------------------------|
  v                             v
Web portal + Control API       Resource Gateway
(vinext + Go API)              (Go, HTTP/WS only)
  |                             |  fixed verified origin
  |                             |  pinned safe IP per connection
  v                             v
PostgreSQL <---- audit ---- Authorized public target
  |
  +---- optional Redis: one-time tickets, distributed limits, revocation
```

职责必须分离：

| 组件 | MVP 职责 |
|---|---|
| 现有 vinext/Sites 项目 | 英文落地页、登录后门户、URL 输入、资源与权限 UI；不直接承担任意出站代理 |
| Control API | 身份映射、资源注册、验证、策略、一次性 launch ticket、审计查询 |
| Resource Gateway | 消费 ticket、建立资源会话、逐请求授权、HTTP/WS 流式转发、限流 |
| PostgreSQL | 用户、组、资源、验证、ACL、设备/会话、审计元数据 |
| Redis（可选） | 60 秒一次性 ticket、撤销、跨实例并发数与令牌桶；单实例阶段可用数据库替代 |
| Edge/WAF | TLS、DDoS 基础保护、登录前的 IP 限制；不是最终授权来源 |

建议公网域名：

```text
app.example.com                 English portal
api.example.com                 Control API
r-{opaque_resource_id}.access.example.com
                                one isolated browser origin per resource
```

使用 `*.access.example.com` 通配 DNS 和证书。`opaque_resource_id` 必须是随机公共 ID，不能使用资源域名、用户 ID 或递增数据库 ID。

### 3.1 身份信任边界

Control API 只接受签名且 `aud` 明确为该 API 的短期 OIDC access token，或来自同源 portal BFF 的签名断言。若现有 Sites 部署使用平台注入的 `oai-authenticated-user-*` headers，这些 headers 只能在受信的 Sites 边缘之后读取；portal 不得把浏览器提供的同名 headers 原样转给 Control API，API/Gateway 入口也必须删除它们。

浏览器 Cookie 会话的写操作必须同时校验 `Origin`、CSRF token 和 `SameSite` 策略，Control API 不开放通配 CORS。数据平面身份不复用 Control API bearer token，而是使用与单一资源子域绑定的短期 Gateway session，从而缩小 token 泄漏后的可访问范围。

## 4. 数据模型与授权

建议最小实体：

| 实体 | 关键字段 |
|---|---|
| `users` | `id`, `issuer`, `subject`, `email`, `email_verified`, `status`, timestamps |
| `groups` / `group_members` | 组和成员关系；MVP 可先只有 owner/member 两种角色 |
| `resources` | `id`, `public_id`, `owner_id`, `display_name`, `scheme`, `ascii_host`, `port`, `status`, `allowed_path_prefixes`, `session_ttl_seconds` |
| `resource_verifications` | `resource_id`, `method`, `token_hash`, `expires_at`, `verified_at`, evidence metadata |
| `resource_grants` | `resource_id`, `subject_type`, `subject_id`, `actions`, `starts_at`, `expires_at`, `revoked_at` |
| `launch_tickets` | `ticket_hash`, `user_id`, `resource_id`, encrypted destination path/query/fragment, `expires_at`, `consumed_at` |
| `gateway_sessions` | `id`, `user_id`, `resource_id`, `expires_at`, `last_seen_at`, `revoked_at` |
| `audit_events` | actor, resource, action, decision, reason, network metadata, byte counts, trace ID, timestamps |

### 4.1 资源身份

一条 Web 资源在 MVP 中严格等于一个规范化 origin：

```text
(scheme, ASCII hostname, effective port)
https://lab.example.org:8443
```

- 默认不支持通配域名；每个 origin 独立验证和授权。
- `https://example.com` 与 `https://example.com:443` 是同一 origin。
- `http://example.com` 与 `https://example.com` 是两个资源。
- 路径不属于资源身份，但可由 `allowed_path_prefixes` 进一步收窄。
- 一个资源可配置少量经过验证的同源别名；不得把重定向目标自动加入别名。

### 4.2 所有权验证

资源创建后状态为 `pending_verification`，在验证前不可代理：

1. DNS TXT：`_authorized-access.<host> = aa-verification=<random-token>`。
2. HTTP 文件：在该精确 origin 的 `/.well-known/authorized-access-challenge` 返回一次性 token；验证器仍执行完整 SSRF 检查。

DNS 验证证明域名控制权，但每个额外端口仍必须被资源清单显式列出。验证应每 30 天续期，DNS/证书或目标地址出现高风险变化时可提前要求复验。

公开托管服务默认不允许通过网关直连 RFC1918、环回、链路本地或云元数据地址。后续私有资源必须通过部署在资源网络内的出站连接器访问；“已验证某个公网域名”不能成为访问网关本机或私网的理由。

### 4.3 授权模型

MVP 使用简单 RBAC + 资源约束：

```text
subject (user/group)
  -> action (resource:launch, resource:http, resource:websocket, resource:admin)
  -> resource_id
  -> constraints (path prefixes, methods, valid time, max session TTL)
```

默认拒绝。资源 owner 只自动拥有 `resource:admin`，是否拥有数据访问也应由明确 grant 表达，便于审计。任何资源停用、grant 撤销或用户封禁应在 30 秒策略缓存窗口内生效，新的连接立即拒绝。

## 5. URL `host:port` 解析规范

所有入口必须调用同一个经过单元测试和模糊测试的规范化库。不得用正则拆 URL，也不得用字符串拼接构造上游地址。

### 5.1 接受与规范化

1. 去除输入两端空白，限制原始输入 2,048 字节。
2. 若缺少 scheme，可在 UI 中补 `https://` 并明确显示规范化结果；API 本身要求显式 `http://` 或 `https://`。
3. 使用标准 URL parser；仅允许 `http` 和 `https`。
4. 拒绝 username/password、控制字符、反斜杠歧义、空 hostname 和非法 percent encoding。
5. hostname 去掉末尾点，按 UTS #46/IDNA 转为小写 ASCII；展示时可显示 Unicode，但策略键只用 ASCII。
6. IPv6 必须为方括号形式。公开服务 MVP 默认拒绝所有 IP literal；确需支持时只能允许验证后的全局单播地址。
7. 端口必须为十进制 `1..65535`；拒绝带符号、空端口、十六进制或其他混淆写法。
8. 无端口时 `http=80`、`https=443`，并从规范化展示 URL 中省略默认端口。
9. 移除 dot segments；fragment 不发送给上游，只保存在 launch 的浏览器跳转状态中。
10. 匹配资源时只使用规范化 `(scheme, ascii_host, effective_port)`；路径和 query 不能影响目标主机。

示例：

| 输入 | 结果 |
|---|---|
| `example.com/docs` | UI 规范化为 `https://example.com/docs`；API 直接调用则拒绝缺少 scheme |
| `https://EXAMPLE.com.:443/a/../b` | origin `https://example.com:443`，path `/b` |
| `https://example.com:8443/` | origin `https://example.com:8443` |
| `https://user:pass@example.com/` | 拒绝 `URL_CREDENTIALS_NOT_ALLOWED` |
| `file:///etc/passwd` | 拒绝 `SCHEME_NOT_ALLOWED` |
| `http://127.0.0.1/` | 拒绝 `IP_LITERAL_NOT_ALLOWED` |
| `https://example.com:0/` | 拒绝 `PORT_NOT_ALLOWED` |

### 5.2 重定向

默认不由服务器自动跟随上游重定向：

- 同一已验证 origin 的 `Location` 改写为当前资源代理域名。
- 指向资源已配置并验证别名的重定向，可签发新的同用户 launch。
- 其他 origin 返回一个网关确认页；只有该 origin 也是用户有权访问的资源时才能继续。
- 验证器最多跟随 3 次重定向，且每跳都重新规范化、授权、解析 DNS 和检查 IP。

## 6. 浏览器启动和代理流程

```text
1. Portal POST /v1/launches { input_url }
2. Control API parses URL and finds exact active resource
3. Policy allows user + method/path/session constraints
4. API stores a hashed, one-use, 60-second launch ticket
5. Browser navigates to https://r-<id>.access.example.com/_launch/<ticket>
6. Gateway atomically consumes ticket and sets host-only __Host-aa_session
7. Gateway redirects to the requested path/query/fragment on the same resource host
8. Each request reconstructs upstream URL from the resource's fixed origin + request path/query
9. Gateway rechecks policy, resolves and pins a safe IP, then streams the request
```

关键约束：

- 数据平面 URL 不接受 `?url=https://...` 形式的目标参数。
- launch ticket 128 位以上随机、一次性、60 秒过期；数据库只存哈希。
- ticket 中的目标状态服务端加密存储，ticket 本身不承载明文 URL。
- `/_launch/` 响应设置 `Referrer-Policy: no-referrer`、`Cache-Control: no-store`。
- `__Host-aa_session` 使用 `Secure; HttpOnly; SameSite=Lax; Path=/`，且绝不设置父域 `Domain`。
- 每个资源使用独立子域，因此目标应用 Cookie 不会跨资源共享。
- 网关保留 `__Host-aa_` / `__aa_` 前缀；同名上游 Cookie 被拒绝。

### 6.1 HTTP 处理

- 拒绝 absolute-form、authority-form 和非预期 upgrade；上游 origin 永远来自数据库资源记录。
- 入口层拒绝重复 `Host`、同时存在冲突 `Content-Length`/`Transfer-Encoding`、非法 header 名、obs-fold 和超过 32 KiB 的 headers；边缘代理与 Gateway 必须有一致的请求边界，避免 request smuggling。
- 移除 hop-by-hop headers、客户端提供的 `Forwarded` / `X-Forwarded-*` / `Proxy-*`。
- 上游 `Host` 固定为已验证目标 host:port，TLS SNI 固定为资源 hostname。
- 可注入经过清洗的 `Forwarded`，但默认不传终端用户公网 IP，优先保护隐私。
- 将代理 origin 的 `Origin` / `Referer` 安全映射回目标 origin，以兼容同源 CSRF 检查；映射前必须精确解析，不做字符串替换。
- `Set-Cookie` 的 `Domain` 仅在等于目标 host 时移除，使其成为资源代理 host-only Cookie；不接受向父域或其他域设置 Cookie。
- 同源 `Location` 映射回资源代理 host；不改写响应正文。
- 流式传输请求/响应并实施字节计数，不把完整正文缓存在内存。
- 默认请求正文上限 16 MiB、响应上限 64 MiB；大文件需资源级显式策略。
- 上游连接超时 10 秒、响应头超时 20 秒、普通请求总时长 120 秒，均可在安全上限内按资源调整。
- 始终校验上游 TLS 证书和 hostname；没有 `insecure_skip_verify` 开关。
- 代理响应带 `Cache-Control: private, no-store`，MVP 不缓存认证内容。

### 6.2 WebSocket

- 在返回 `101` 前完成身份、grant、Origin、并发数和目标 IP 检查。
- 只允许资源明确开启 `resource:websocket`。
- 首版可禁用 `permessage-deflate`，降低压缩炸弹与内存风险。
- 双向计量，默认空闲 5 分钟、最长 60 分钟、每用户 2 条并发连接。
- grant 撤销后最多在 30 秒内关闭连接；超额时以明确 close code 终止。
- 不能用 WebSocket 消息选择新的 host 或 port。

## 7. API 契约

所有 Control API 返回 `application/json`，错误使用稳定的机器码：

```json
{
  "error": {
    "code": "RESOURCE_NOT_AUTHORIZED",
    "message": "This resource is not available to your account.",
    "request_id": "req_01J..."
  }
}
```

为避免枚举，未注册资源和无权查看的资源对普通用户都可返回同一个 `RESOURCE_NOT_AUTHORIZED`。只有资源创建流程才说明需要验证。

### 7.1 OpenAPI 风格摘要

```yaml
openapi: 3.1.0
info:
  title: Authorized Access Control API
  version: 0.1.0
servers:
  - url: https://api.example.com
security:
  - oidcSession: []
paths:
  /v1/url/normalize:
    post:
      summary: Parse only; performs no outbound request
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [input_url]
              properties:
                input_url: { type: string, maxLength: 2048 }
      responses:
        '200':
          description: Normalized URL parts
          content:
            application/json:
              example:
                normalized_url: https://lab.example.org:8443/dashboard
                origin: https://lab.example.org:8443
                scheme: https
                host: lab.example.org
                port: 8443
                path: /dashboard
        '422': { description: URL is malformed or uses a prohibited form }

  /v1/resources:
    get:
      summary: List resources visible to the current user
      responses:
        '200': { description: Resource list; never returns verification token }
    post:
      summary: Register an origin in pending_verification state
      requestBody:
        required: true
        content:
          application/json:
            example:
              display_name: Lab Console
              origin: https://lab.example.org:8443
              allowed_path_prefixes: ["/"]
              websocket_enabled: true
      responses:
        '201': { description: Pending resource and available verification methods }
        '409': { description: Origin already registered; ownership recovery flow required }
        '422': { description: Origin is not eligible for public gateway registration }

  /v1/resources/{resource_id}/verification-challenges:
    post:
      summary: Rotate and issue an ownership challenge
      parameters:
        - in: path
          name: resource_id
          required: true
          schema: { type: string }
      requestBody:
        content:
          application/json:
            example: { method: dns_txt }
      responses:
        '201':
          description: One-time challenge; shown only to resource administrators

  /v1/resources/{resource_id}/verify:
    post:
      summary: Run the selected verification using the safe verifier
      responses:
        '200': { description: Resource is active }
        '409': { description: Challenge not yet observed }
        '422': { description: Resolved target failed egress safety policy }

  /v1/resources/{resource_id}/grants:
    post:
      summary: Grant a user or group access; resource admin only
      requestBody:
        content:
          application/json:
            example:
              subject_type: user
              subject_id: usr_01J...
              actions: [resource:launch, resource:http, resource:websocket]
              expires_at: 2026-09-01T00:00:00Z
      responses:
        '201': { description: Grant created and audited }

  /v1/launches:
    post:
      summary: Create a one-time launch for an already authorized resource
      requestBody:
        required: true
        content:
          application/json:
            example:
              input_url: https://lab.example.org:8443/dashboard?view=health#services
      responses:
        '201':
          description: Short-lived, single-use launch URL
          content:
            application/json:
              example:
                launch_url: https://r-B6dK2x.access.example.com/_launch/4aY...opaque
                expires_at: 2026-08-13T03:05:00Z
                resource:
                  id: res_01J...
                  display_name: Lab Console
        '403': { description: Resource is unavailable to this user }
        '422': { description: URL or requested path is prohibited }
        '429': { description: Fair-use limit exceeded }

  /v1/sessions:
    get:
      summary: List the user's active gateway sessions
      responses:
        '200': { description: Active sessions }
  /v1/sessions/{session_id}:
    delete:
      summary: Revoke one session
      responses:
        '204': { description: Revoked }

  /v1/audit-events:
    get:
      summary: Resource admins see events only for resources they administer
      responses:
        '200': { description: Paginated privacy-preserving events }

  /health/live:
    get:
      security: []
      responses:
        '200': { description: Process is alive; exposes no dependency details }
  /health/ready:
    get:
      security: []
      responses:
        '200': { description: Ready behind private load-balancer checks only }
components:
  securitySchemes:
    oidcSession:
      type: openIdConnect
      openIdConnectUrl: https://identity.example.com/.well-known/openid-configuration
```

数据平面不是一个通用 OpenAPI proxy endpoint。唯一的特殊路由是：

```text
GET /_launch/{one_time_ticket}   consume ticket, set host-only session, 303 redirect
ANY /{resource_relative_path}    proxy only to this subdomain's fixed resource origin
WS  /{resource_relative_path}    upgrade only when the same resource policy permits
```

## 8. 策略与连接伪代码

```go
func CreateLaunch(actor User, raw string) (Launch, error) {
    u := CanonicalParse(raw) // http(s), no credentials, normalized host/port/path
    if u.HasIPLiteral || !AllowedPublicPort(u.Port) {
        return Deny("URL_NOT_ELIGIBLE")
    }

    resource := Resources.FindActiveByOrigin(u.Scheme, u.ASCIIHost, u.Port)
    // Same outward error for missing and invisible resources.
    if resource == nil || !Policy.Allows(actor, "resource:launch", resource) {
        AuditDecision(actor, resource, "launch", "deny", "not_authorized")
        return Deny("RESOURCE_NOT_AUTHORIZED")
    }
    if !PathHasSegmentPrefix(u.Path, resource.AllowedPathPrefixes) {
        return Deny("PATH_NOT_ALLOWED")
    }
    if !Quota.AllowLaunch(actor.ID, resource.ID) {
        return Deny("RATE_LIMITED")
    }

    ticket := RandomBytes(32)
    StoreTicket(SHA256(ticket), actor.ID, resource.ID,
        Encrypt(u.Path, u.RawQuery, u.Fragment), now.Add(60*time.Second))
    return LaunchURL(resource.PublicID, ticket), nil
}

func ProxyRequest(req Request, resource Resource, session Session) Response {
    Require(session.ResourceID == resource.ID && !session.ExpiredOrRevoked())
    Require(Policy.Allows(session.User, "resource:http", resource))
    Require(req.TargetForm == OriginForm) // reject absolute/authority form
    Require(PathHasSegmentPrefix(req.URL.Path, resource.AllowedPathPrefixes))
    Require(req.Method != "CONNECT" && req.Method != "TRACE")

    // Never resolve a destination supplied in path, query, Host, Origin, or message body.
    target := URL{Scheme: resource.Scheme, Host: resource.ASCIIHost,
                  Port: resource.Port, Path: req.URL.Path, Query: req.URL.Query}

    answers := Resolver.ResolveAllWithCNAMEChain(resource.ASCIIHost)
    Require(len(answers) > 0)
    for _, ip := range answers {
        Require(EgressPolicy.IsGlobalPublic(ip))
        Require(!EgressPolicy.IsMetadataOrPlatformIP(ip))
    }
    chosen := RandomHealthy(answers)

    // Dial the checked address directly to prevent DNS rebinding between check and connect.
    conn := DialPinned(chosen, resource.Port,
        TLS{ServerName: resource.ASCIIHost, VerifyCertificate: true})
    return StreamSanitized(req, target, conn, resource.Limits)
}
```

`PathHasSegmentPrefix` 必须按解码后的路径段比较，不能用简单字符串 `HasPrefix`，否则 `/allowed-evil`、双重编码和 `%2f` 可能越过 `/allowed` 策略。请求路由和策略库必须使用同一种规范化形式。

## 9. SSRF 与出口防护

SSRF 防护是多层强制条件，不是一个 hostname 黑名单：

1. **输入层**：只接受标准 `http(s)` URL；拒绝 credentials、IP literal、非规范端口、歧义反斜杠、控制字符和过长输入。
2. **资源层**：只有已验证、active 的固定 origin 可访问；浏览器请求不能改变 origin。
3. **DNS 层**：使用受控 resolver，限制 CNAME 深度，检测循环；每次新连接重新解析。
4. **地址层**：拒绝非全局地址、环回、私网、链路本地、运营商级 NAT、组播、保留/文档网段、IPv4-mapped IPv6，以及所有云元数据和平台内部地址。
5. **连接层**：检查后将选中的 IP 固定给 dialer，TLS SNI/证书校验仍使用资源 hostname，消除检查后再次解析造成的 rebinding。
6. **重定向层**：不自动跨 origin；每跳重新执行全部检查。
7. **协议层**：无通用 CONNECT，无用户自定义 `Host`，无 gopher/file/ftp，自定义 upgrade 只允许 WebSocket。
8. **网络层**：容器/VM 出口防火墙再次拒绝私网、元数据、控制面数据库和编排网络；即使应用检查失误也无法到达。
9. **平台层**：管理 API 和数据库使用独立网络；网关工作负载没有云控制面凭据。

至少明确拒绝：

```text
IPv4: 0.0.0.0/8, 10/8, 100.64/10, 127/8, 169.254/16,
      172.16/12, 192.168/16, benchmarking/documentation/reserved ranges,
      multicast and 240/4
IPv6: ::/128, ::1/128, IPv4-mapped forms, fc00/7, fe80/10,
      documentation ranges, multicast and other non-global scopes
Special: 169.254.169.254 and provider-specific metadata aliases/endpoints
```

不要手写一份永不更新的 CIDR 表。实现应依赖标准 IP 分类，加平台维护的 denyset，并在 CI 中覆盖常见 SSRF 绕过向量。出站防火墙规则必须通过集成测试验证。

## 10. 免费公共服务的防滥用

建议初始公平使用额度；上线后根据真实成本调整，并在 UI 明示：

| 维度 | 初始值 |
|---|---:|
| 匿名请求 | 仅落地页和登录；不允许 launch/proxy |
| 登录前 API | 每 IP 10 请求/分钟，突发 20 |
| 每用户 launch | 5/分钟，突发 10 |
| 每用户 HTTP | 60 请求/分钟，突发 30 |
| 每用户并发 HTTP | 6 |
| 每用户并发 WebSocket | 2 |
| 每账户资源数 | 5 个 active resources |
| 每用户日流量 | 250 MiB，上下行合计 |
| 单请求正文/响应 | 16 MiB / 64 MiB |
| WebSocket | 空闲 5 分钟，最长 60 分钟 |
| launch/session TTL | ticket 60 秒；session 默认 30 分钟，最长 8 小时 |

限制键至少包含 `edge IP`、`account`、`resource`、`session` 四层，避免共享 IP 误伤也避免换账号绕过。超额返回 `429`、`Retry-After` 和英文可解释信息。

第一版防滥用还应包含：

- 验证邮箱/OIDC 身份；高风险注册或异常流量时才触发 CAPTCHA/人工复核。
- 每个目标必须完成控制权验证；禁止匿名者把平台用作反射或扫描器。
- 端口默认仅 `80, 443, 8080, 8443`；其他 Web 端口经风险复核开放。Web MVP 不提供任意 TCP 端口。
- 每资源和目标 IP 的并发/带宽断路器，避免打垮小型实验室服务。
- 账户、资源、ASN/IP 和设备信号的异常检测；支持临时冻结而非立即删除证据。
- 可公开访问的 abuse 联系方式、快速资源停用和申诉流程。
- 禁止自动扫描、批量抓取、凭证填充、垃圾邮件、挖矿、DDoS 和违反目标服务条款的行为。
- 不通过“隐蔽流量”降低封禁概率；被目标或网络策略明确拒绝时停止重试。

## 11. 审计、隐私与可观测性

每个决策记录结构化事件：

```json
{
  "occurred_at": "2026-08-13T03:04:05.120Z",
  "request_id": "req_01J...",
  "trace_id": "tr_01J...",
  "actor_id": "usr_01J...",
  "session_id": "ses_01J...",
  "resource_id": "res_01J...",
  "action": "http.request",
  "method": "GET",
  "path_hash": "sha256:...",
  "decision": "allow",
  "reason": "grant_active",
  "status_code": 200,
  "bytes_in": 418,
  "bytes_out": 8042,
  "duration_ms": 132,
  "client_ip_prefix": "203.0.113.0/24",
  "gateway_region": "sg-1"
}
```

- 默认不记录 query、fragment、Cookie、Authorization、请求/响应正文、密码或表单值。
- path 默认存 HMAC/hash；资源管理员明确需要时可选择记录截断路径，但 UI 必须提示隐私影响。
- 客户端 IP 只保留截断前缀或 keyed hash；安全事件可进入访问受控的短期原始日志。
- 普通审计事件保留 30 天，聚合用量 90 天；删除账户时按法律与反滥用最小需求处理。
- 管理员读取审计日志本身也要审计。
- 指标至少包括允许/拒绝计数、错误码、p50/p95/p99 延迟、连接数、DNS/TLS 失败、字节、限流与每资源健康度。
- trace ID 可跨 portal、Control API 与 Gateway 关联，但不得把 launch ticket 放入日志。

告警初始阈值：SSRF deny 突增、单资源错误率 > 20%、网关 5xx > 2%、带宽异常增长、数据库/Redis 不可用、验证器访问 denyset、证书即将过期。

## 12. 部署拓扑

### 12.1 可执行的单区域 MVP

```text
Public Internet
   |
   v
Cloudflare DNS/WAF or equivalent
   |
   +--> app.example.com  -> current vinext/Sites deployment
   +--> api.example.com  -> Caddy/Envoy -> control-api:8080
   +--> *.access.example.com -> Caddy/Envoy -> gateway:8081

Private container network
   control-api ---- PostgreSQL
   gateway -------- PostgreSQL (least-privilege read/session/audit role)
       |------------ Redis (optional)
       |
       +-- egress firewall/NAT --> verified public HTTP(S) targets only
```

推荐仓库后续目录，但本文不创建实现文件：

```text
/app                    existing English web portal
/services/control-api   Go control plane
/services/gateway       Go data plane
/internal/urlpolicy     canonical parser + IP policy shared package
/deploy/compose         local/single-VM deployment
/deploy/firewall        tested egress policy
```

单 VM 可以使用 Docker Compose 验证产品，但至少满足：

- PostgreSQL 和 Redis 不绑定公网端口。
- API/Gateway 使用不同数据库角色；Gateway 不能修改资源所有权或 grant。
- Gateway 容器只读文件系统、非 root、最小 Linux capabilities、无 Docker socket、无云 IAM 凭据。
- 入口 TLS 只开放 443；80 仅用于重定向或 ACME。
- 出站防火墙默认拒绝后按已定义的公共 Web 目标策略放行。
- 密钥来自 secrets manager 或 root-only 环境文件，不进入镜像与 Git。
- 每日加密备份 PostgreSQL，并至少演练一次恢复。

规模扩大后将 Control API、Gateway、PostgreSQL 拆开；Gateway 可横向扩展，ticket 消费和并发计数必须原子化。无需在 MVP 过早引入 Kubernetes。

### 12.2 私有资源的后续安全路线

私有 IP、实验室内网和非公网 DNS 不应通过放宽 SSRF denylist实现。后续增加一个由资源所有者部署的 connector：

```text
private target <- local policy <- outbound-only connector
                                    || mTLS authenticated stream
                                    v
                              public gateway :443
```

connector 只接受绑定 `resource_id` 的签名请求，并在本地再执行 host/port allowlist。这样公网 Gateway 永远不能自由扫描用户私网。

## 13. 里程碑与验收

### M0：威胁模型与契约（2–3 天）

交付：资源/授权模型、URL 规范、错误码、滥用条款、日志数据分类。

验收：

- 明确“免费但登录、已验证资源、默认拒绝”的产品文案。
- 安全评审确认没有匿名代理、CONNECT、任意目标 fallback。
- OpenAPI 契约和数据库迁移方案通过评审。

### M1：规范化、注册和验证（4–6 天）

交付：共享 URL policy 库、资源 CRUD、DNS/HTTP challenge、grant、审计基础。

验收：

- 表中所有 URL 示例有自动测试。
- 至少 100 个恶意 URL/SSRF corpus 与 parser fuzz 测试不崩溃、不误放行。
- DNS rebinding 集成测试证明 dial 到的是已检查并 pinned 的 IP。
- 未验证资源无法创建 launch；验证 token 不出现在普通列表或日志。

### M2：HTTP Gateway（5–7 天）

交付：一次性 ticket、host-only 会话、HTTP 流式代理、header/Cookie/Location 处理、限流。

验收：

- 两个已授权 HTTPS 应用和一个 `:8443` 应用可通过 URL 输入启动。
- 相对导航、表单、同源 API 和登录 Cookie 工作，资源间 Cookie 完全隔离。
- 未授权端口、路径、资源、absolute-form 请求和跨 origin redirect 被明确拒绝。
- 断开客户端后上游请求被取消；正文不会整体进入内存。
- 上游无效证书、DNS 变私网、metadata 地址均 fail closed。

### M3：WebSocket 与门户集成（3–5 天）

交付：WS upgrade、配额/并发、英文状态与错误 UI、资源注册/分享页面。

验收：

- 已授权 WS echo/真实应用可连续运行 30 分钟。
- 未授权 Origin、超并发和撤销 grant 会拒绝或在 30 秒内关闭。
- 键盘操作、屏幕阅读器标签、移动窄屏和错误恢复通过基本可访问性测试。
- UI 不声称可访问任意网站；未授权 URL 引导至安全注册或申请权限流程。

### M4：上线加固（4–6 天）

交付：出口防火墙、配额仪表、abuse/停用流程、备份恢复、告警和 runbook。

验收：

- 外部安全测试覆盖 SSRF、DNS rebinding、request smuggling、header injection、Cookie 隔离和 WebSocket 资源耗尽。
- Gateway 即使被模拟攻陷也无法访问数据库管理口、云元数据或容器编排网络。
- `429`、上游超时、资源撤销、Redis/数据库短暂故障均 fail closed 且返回稳定错误码。
- 完成 24 小时小流量 soak test，无连接泄漏，内存和 goroutine 数稳定。
- 能在 15 分钟内全局停用一个资源或用户，并保留最小必要审计证据。

## 14. 上线前必须解决的决策

1. 身份源：Sign in with ChatGPT/OIDC、邮箱 magic link，还是两者并存。
2. 资源争议：同一 origin 已被注册时的所有权恢复和转移流程。
3. 公共端口：是否严格只开放 `80/443/8080/8443`，以及例外审批人。
4. 数据驻留：首个区域、审计保留天数和 abuse 证据访问角色。
5. 成本上限：每用户/资源流量额度、总月度出口预算与达到预算后的降级策略。
6. 兼容性：首批两个真实授权应用是否能配置 external base URL、OAuth callback 与 WebSocket origin。
7. 私有资源：何时进入 connector 阶段；在此之前不得通过开放私网 CIDR 临时绕过。

完成这些决策后，MVP 的实现顺序应是：先规范化与验证，再授权与 launch ticket，最后才接入真实出站转发。这样即使前端 URL 输入框先上线，也不会短暂形成开放代理。
