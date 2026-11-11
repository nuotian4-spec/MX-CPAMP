// 密码/TOTP、Cookie、工作区核心取自 toSub2 8548397e89bf，保留 MIT 许可。
import crypto from 'node:crypto';
const DEFAULT_TIMEOUT_MS = 30000;
const browserHeadersForTransport = transport => transport.headers;
const safeUrl = value => new URL(value).origin;
const summarizeHtml = () => '';
const ask = async () => { throw new Error('interaction_required'); };
const observeStage = (onStage, stage) => { try { onStage?.(stage); } catch {} };
const interaction = reason => Object.assign(new Error('interaction_required'), {diagnostic_reason: reason});
const askTotpOtp = ask;
const verifyEmailOtp = ask;
const completeAccountProfileIfNeeded = async (_client, {payload}) => {
  if (isAccountProfileRequired(payload)) throw interaction('account_setup_required');
  return payload;
};
const isLocalCallback = value => {
  try { const u=new URL(value); return !u.username && !u.password && (u.origin==='http://localhost:1455' && u.pathname==='/auth/callback' || u.origin==='https://bps.openai.com' && u.pathname==='/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback'); } catch { return false; }
};
const createSentinelHeaders = async (client, {deviceId,flow,authBase}) => {
  const tokens=await client.transport.sentinel({deviceId,flow,pageUrl:authBase+'/log-in/password'});
  return {'openai-sentinel-token':tokens.token,...(tokens.soToken ? {'openai-sentinel-so-token':tokens.soToken}: {})};
};
class CookieJar {
  constructor(cookies = []) {
    this.cookies = Array.isArray(cookies)
      ? cookies.filter(isStoredCookie).map((cookie) => ({ ...cookie }))
      : [];
  }

  setFromResponse(url, headers) {
    for (const line of getSetCookie(headers)) {
      const cookie = parseSetCookie(line, url);
      if (!cookie) continue;
      const idx = this.cookies.findIndex(
        (item) =>
          item.name === cookie.name &&
          item.domain === cookie.domain &&
          item.path === cookie.path,
      );
      if (cookie.expires && cookie.expires <= Date.now()) {
        if (idx >= 0) this.cookies.splice(idx, 1);
        continue;
      }
      if (idx >= 0) this.cookies[idx] = cookie;
      else this.cookies.push(cookie);
    }
  }

  headerFor(url) {
    const target = new URL(url);
    const now = Date.now();
    this.cookies = this.cookies.filter((item) => !item.expires || item.expires > now);
    return this.cookies
      .filter((item) => cookieMatches(item, target))
      .map((item) => `${item.name}=${item.value}`)
      .join("; ");
  }

  namesFor(url) {
    const target = new URL(url);
    const now = Date.now();
    this.cookies = this.cookies.filter((item) => !item.expires || item.expires > now);
    return this.cookies
      .filter((item) => cookieMatches(item, target))
      .map((item) => item.name);
  }

  has(name) {
    return this.cookies.some((item) => item.name === name);
  }

  value(name, url = null) {
    const target = url ? new URL(url) : null;
    const found = this.cookies.find((item) => item.name === name && (!target || cookieMatches(item, target)));
    return found?.value || null;
  }

  merge(cookies) {
    for (const rawCookie of Array.isArray(cookies) ? cookies : []) {
      if (!isStoredCookie(rawCookie)) continue;
      const cookie = { ...rawCookie, domain: rawCookie.domain.replace(/^\./, "").toLowerCase() };
      const idx = this.cookies.findIndex(
        (item) => item.name === cookie.name && item.domain === cookie.domain && item.path === cookie.path,
      );
      if (cookie.expires && cookie.expires <= Date.now()) {
        if (idx >= 0) this.cookies.splice(idx, 1);
      } else if (idx >= 0) this.cookies[idx] = cookie;
      else this.cookies.push(cookie);
    }
  }

  toJSON() {
    return this.cookies;
  }

  static fromJSON(cookies) {
    return new CookieJar(cookies);
  }
}

class ProtocolClient {
  constructor(options = {}) {
    this.jar = options.jar || new CookieJar();
    this.verbose = Boolean(options.verbose);
    this.debugAuth = Boolean(options.debugAuth);
    this.transport = options.transport || null;
  }

  async rawFetch(url, options = {}) {
    if (this.transport) return this.transport.fetch(url, options);
    return fetch(url, options);
  }

  async request(method, url, options = {}) {
    const targetUrl = new URL(url);
    const headers = new Headers(options.headers || {});
    for (const [name, value] of Object.entries(browserHeadersForTransport(this.transport))) {
      headers.set(name, value);
    }
    if (options.userAgent) headers.set("user-agent", options.userAgent);

    if (!headers.has("accept")) {
      headers.set("accept", options.accept || "application/json, text/plain, */*");
    }
    if (options.referer) headers.set("referer", options.referer);
    if (options.origin) headers.set("origin", options.origin);

    const cookie = this.jar.headerFor(url);
    if (cookie) headers.set("cookie", cookie);

    if (
      method !== "GET" &&
      (targetUrl.hostname === "auth.openai.com" || targetUrl.pathname.startsWith("/api/accounts/"))
    ) {
      headers.set("x-access-flow-invocation-id", crypto.randomUUID());
      headers.set("sec-fetch-site", "same-origin");
      headers.set("sec-fetch-mode", "cors");
      headers.set("sec-fetch-dest", "empty");
      headers.set("priority", "u=1, i");
      if (options.json) headers.set("accept", "application/json");
    }

    let body = options.body;
    if (options.json) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(options.json);
    } else if (options.form) {
      headers.set("content-type", "application/x-www-form-urlencoded");
      body = new URLSearchParams(options.form).toString();
    }

    if (this.verbose) {
      console.error(`> ${method} ${safeUrl(url)}`);
    }

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), options.timeoutMs || DEFAULT_TIMEOUT_MS);
    let res;
    let text;
    try {
      res = await this.rawFetch(url, {
        method,
        headers,
        body,
        redirect: "manual",
        signal: controller.signal,
        timeoutMs: options.timeoutMs || DEFAULT_TIMEOUT_MS,
      });
      this.jar.setFromResponse(url, res.headers);
      this.jar.merge(res.headers.transportCookies);
      text = await res.text();
    } catch (error) {
      if (error?.name === "AbortError") {
        throw new Error(`${method} ${safeUrl(url)} timed out after ${options.timeoutMs || DEFAULT_TIMEOUT_MS}ms`);
      }
      throw error;
    } finally {
      clearTimeout(timeout);
    }

    const location = res.headers.get("location");
    if (this.verbose) {
      const suffix = location ? ` -> ${safeUrl(new URL(location, url).toString())}` : "";
      console.error(`< ${method} ${safeUrl(url)} ${res.status}${suffix}`);
    }

    if (isRiskControlResponse(res, text)) {
      throw new Error(
        `PROXY_RISK_CONTROL: ${method} ${safeUrl(url)} returned HTTP ${res.status} with a security-check page`,
      );
    }

    return { res, text, location: location ? new URL(location, url).toString() : null, url };
  }

  async follow(url, options = {}) {
    let current = url;
    let last = null;
    for (let i = 0; i < (options.maxRedirects || 12); i += 1) {
      if (isLocalCallback(current)) return { finalUrl: current, last };
      last = await this.request("GET", current, {
        accept:
          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
        referer: options.referer,
      });
      if (last.res.status >= 400) assertOk(last, `GET ${current}`);
      if (![301, 302, 303, 307, 308].includes(last.res.status) || !last.location) {
        return { finalUrl: current, last };
      }
      current = last.location;
    }
    throw new Error(`Too many redirects from ${url}`);
  }

  async getJson(method, url, options = {}) {
    const result = await this.request(method, url, options);
    assertOk(result, `${method} ${url}`);
    try {
      return { data: JSON.parse(result.text), result };
    } catch {
      throw new Error(`Expected JSON from ${url}, got: ${result.text.slice(0, 160)}`);
    }
  }
}

function getSetCookie(headers) {
  if (Array.isArray(headers.rawSetCookie)) return headers.rawSetCookie;
  if (typeof headers.getSetCookie === "function") return headers.getSetCookie();
  const combined = headers.get("set-cookie");
  if (!combined) return [];
  return combined.split(/,(?=\s*[^;,=\s]+=)/g).map((item) => item.trim());
}

function parseSetCookie(line, requestUrl) {
  const target = new URL(requestUrl);
  const parts = line.split(";").map((part) => part.trim());
  const [nameValue, ...attrs] = parts;
  const splitAt = nameValue.indexOf("=");
  if (splitAt <= 0) return null;
  const cookie = {
    name: nameValue.slice(0, splitAt),
    value: nameValue.slice(splitAt + 1),
    domain: target.hostname,
    path: "/",
    secure: false,
    expires: null,
  };
  for (const attr of attrs) {
    const [rawKey, ...rawValue] = attr.split("=");
    const key = rawKey.toLowerCase();
    const value = rawValue.join("=");
    if (key === "domain" && value) cookie.domain = value.replace(/^\./, "").toLowerCase();
    if (key === "path" && value) cookie.path = value;
    if (key === "secure") cookie.secure = true;
    if (key === "expires" && value) {
      const expires = Date.parse(value);
      if (!Number.isNaN(expires)) cookie.expires = expires;
    }
    if (key === "max-age" && value) {
      const seconds = Number(value);
      if (Number.isFinite(seconds)) cookie.expires = Date.now() + seconds * 1000;
    }
  }
  return cookie;
}

function cookieMatches(cookie, target) {
  const domain = cookie.domain.toLowerCase();
  const host = target.hostname.toLowerCase();
  const domainOk = host === domain || host.endsWith(`.${domain}`);
  const pathOk = target.pathname.startsWith(cookie.path || "/");
  const secureOk = !cookie.secure || target.protocol === "https:";
  return domainOk && pathOk && secureOk;
}

function isStoredCookie(cookie) {
  return Boolean(
    cookie &&
    typeof cookie === "object" &&
    typeof cookie.name === "string" &&
    typeof cookie.value === "string" &&
    typeof cookie.domain === "string" &&
    (!cookie.expires || Number.isFinite(Number(cookie.expires))),
  );
}

async function loginChatgptWeb(client, { chatgptBase, authBase, email, rl, password, totpSecret, onStage }) {
  observeStage(onStage, "web_login");
  await client.follow(`${chatgptBase}/`, { referer: `${chatgptBase}/` });
  await client.getJson("GET", `${chatgptBase}/api/auth/providers`, {
    referer: `${chatgptBase}/`,
  });

  const { data: csrf } = await client.getJson("GET", `${chatgptBase}/api/auth/csrf`, {
    referer: `${chatgptBase}/`,
  });
  if (!csrf.csrfToken) throw new Error("Missing csrfToken");
  if (!client.jar.has("__Host-next-auth.csrf-token")) {
    throw new Error("Missing __Host-next-auth.csrf-token cookie; open the ChatGPT home page first or retry.");
  }

  const deviceId = client.jar.value("oai-did", `${chatgptBase}/`) || crypto.randomUUID();
  const authSessionLoggingId = crypto.randomUUID();
  const signinUrl =
    `${chatgptBase}/api/auth/signin/openai?` +
    new URLSearchParams({
      prompt: "login",
      "ext-oai-did": deviceId,
      auth_session_logging_id: authSessionLoggingId,
      screen_hint: "login_or_signup",
      login_hint: email,
    }).toString();

  const { data: signin } = await client.getJson("POST", signinUrl, {
    origin: chatgptBase,
    referer: `${chatgptBase}/`,
    form: {
      callbackUrl: `${chatgptBase}/`,
      csrfToken: csrf.csrfToken,
      json: "true",
    },
  });
  if (!signin.url) throw new Error("signin/openai did not return url");

  const authRequestStartedAt = new Date().toISOString();
  const authPage = await client.follow(signin.url, { referer: `${chatgptBase}/` });
  console.log(`[info] Auth page: ${safeUrl(authPage.finalUrl)}`);
  if (authPage.last?.text) {
    console.log(`[info] Page text: ${summarizeHtml(authPage.last.text)}`);
  }

  let authenticated;
  let loginMethod;
  if (isPasswordLoginPage(authPage.finalUrl)) {
    loginMethod = "password";
    observeStage(onStage, "password");
    console.log("[3/5] Password login page reached.");
    authenticated = await verifyPassword(client, {
      authBase,
      rl,
      deviceId,
      password,
      referer: authPage.finalUrl,
    });
  } else if (isEmailVerificationPage(authPage.finalUrl)) {
    loginMethod = "email_otp";
    observeStage(onStage, "email_otp");
    console.log(`[email-otp-requested-at] ${authRequestStartedAt}`);
    console.log("[3/5] Email OTP page reached. Enter code, or type r to resend.");
    authenticated = await verifyEmailOtp(client, {
      authBase,
      rl,
      deviceId,
      referer: authPage.finalUrl,
    });
  } else if (isCompletedChatgptLoginPage(authPage.finalUrl, chatgptBase, client)) {
    loginMethod = "existing_session";
    authenticated = { continue_url: authPage.finalUrl };
    console.log("[3/5] Existing ChatGPT web session accepted.");
  } else {
    let pathname = authPage.finalUrl;
    try {
      pathname = new URL(authPage.finalUrl).pathname;
    } catch {}
    throw new Error(`UNEXPECTED_LOGIN_PAGE: Expected password or email verification, received ${pathname}`);
  }

  observeStage(onStage, "web_login");
  const mfaRequired = isMfaChallengePayload(authenticated);
  authenticated = await completeTotpMfaIfNeeded(client, {
    authBase,
    rl,
    deviceId,
    payload: authenticated,
    totpSecret,
    onStage,
    referer: authPage.finalUrl,
  });
  observeStage(onStage, "web_login");
  authenticated = await completeAccountProfileIfNeeded(client, {
    authBase,
    deviceId,
    payload: authenticated,
  });
  authenticated = await selectChatgptLoginWorkspaceIfNeeded(client, {
    onStage,
    authBase,
    payload: authenticated,
  });
  observeStage(onStage, "web_login");
  const callback = await continueFlow(client, authenticated);
  return {
    deviceId,
    authSessionLoggingId,
    callbackUrl: callback?.finalUrl || null,
    emailVerified: true,
    loginMethod,
    mfaVerified: mfaRequired,
  };
}

async function selectChatgptLoginWorkspaceIfNeeded(client, { authBase, payload, onStage }) {
  if (!isWorkspaceSelectionPayload(payload)) return payload;
  const workspaceId = pickWorkspaceId(payload);
  if (!workspaceId) {
    console.log("[web] Workspace page did not include a selectable workspace; skipping selection.");
    return payload;
  }
  observeStage(onStage, "workspace_session");
  const workspaceUrl = getContinueUrl(payload) || `${authBase}/workspace`;
  console.log("[web] Select ChatGPT login workspace");
  const { data } = await authJsonStep(client, authBase, "POST", "/api/accounts/workspace/select", {
    workspace_id: workspaceId,
  }, { referer: workspaceUrl });
  return data;
}

async function verifyPassword(client, { authBase, rl, deviceId, password, referer }) {
  let nextPassword = password;
  for (;;) {
    const value = nextPassword || (await ask(rl, "Password (q=quit): "));
    nextPassword = "";
    if (!value || value.toLowerCase() === "q") throw new Error("Stopped before password validation");
    try {
      const sentinelHeaders = await createSentinelHeaders(client, {
        authBase,
        deviceId,
        flow: "password_verify",
      });
      const { data } = await authJsonStep(client, authBase, "POST", "/api/accounts/password/verify", {
        password: value,
      }, { referer, headers: sentinelHeaders });
      console.log("[ok] Password accepted");
      return data;
    } catch (error) {
      if (!isRejectedPasswordError(error)) throw error;
      throw new Error("password_rejected");
      console.log("[warn] Password was rejected. Enter it again, or q to quit.");
    }
  }
}

async function completeTotpMfaIfNeeded(client, { authBase, rl, deviceId, payload, totpSecret, referer, onStage }) {
  if (!isMfaChallengePayload(payload)) return payload;
  observeStage(onStage, "totp");
  const factor = pickTotpFactor(payload);
  if (!factor?.id) throw new Error("2FA is required, but the response does not contain a TOTP factor ID");

  console.log("[mfa] TOTP 2FA challenge reached.");
  await authJsonStep(client, authBase, "POST", "/api/accounts/mfa/issue_challenge", {
    type: "totp",
    id: factor.id,
    force_fresh_challenge: false,
  }, { referer });

  let code;
  let generatedFromSecret = false;
  if (totpSecret) {
    code = generateTotp(totpSecret);
    generatedFromSecret = true;
    console.log("[mfa] Generated a 6-digit code from the configured 2FA key.");
  } else {
    code = await askTotpOtp(rl);
  }
  const challengeUrl = getContinueUrl(payload) || `${authBase}/mfa-challenge/${factor.id}`;
  for (;;) {
    try {
      const sentinelHeaders = await createSentinelHeaders(client, {
        authBase,
        deviceId,
        flow: "password_verify",
      });
      const { data } = await authJsonStep(client, authBase, "POST", "/api/accounts/mfa/verify", {
        type: "totp",
        id: factor.id,
        code,
      }, { referer: challengeUrl, headers: sentinelHeaders });
      console.log("[ok] 2FA verification accepted");
      return data;
    } catch (error) {
      if (!isRejectedTotpError(error)) throw error;
      throw new Error("totp_rejected");
      console.log(
        generatedFromSecret
          ? "[warn] The code generated from the configured 2FA key was rejected; enter a current code manually."
          : "[warn] The 2FA code was rejected; enter a new code.",
      );
      generatedFromSecret = false;
      code = await askTotpOtp(rl);
    }
  }
}

function isRejectedPasswordError(error) {
  return /(?:invalid|incorrect|wrong)[_\s-]*password|password[_\s-]*(?:invalid|incorrect|wrong)/i.test(
    String(error?.message || ""),
  );
}

function isRejectedTotpError(error) {
  return /(?:invalid|incorrect|wrong)[_\s-]*(?:totp|otp|code)|(?:totp|otp|code)[_\s-]*(?:invalid|incorrect|wrong)/i.test(
    String(error?.message || ""),
  );
}

function isPasswordLoginPage(url) {
  try {
    return new URL(url).pathname === "/log-in/password";
  } catch {
    return false;
  }
}

function isEmailVerificationPage(url) {
  try {
    return new URL(url).pathname === "/email-verification";
  } catch {
    return false;
  }
}

function isCompletedChatgptLoginPage(url, chatgptBase, client) {
  try {
    const current = new URL(url);
    const expected = new URL(chatgptBase);
    return current.origin === expected.origin
      && current.pathname === "/"
      && client.jar.has("__Secure-next-auth.session-token");
  } catch {
    return false;
  }
}

function isMfaChallengePayload(payload) {
  if (payload?.page?.type === "mfa_challenge") return true;
  try {
    return new URL(getContinueUrl(payload)).pathname.startsWith("/mfa-challenge/");
  } catch {
    return false;
  }
}

function isWorkspaceSelectionPayload(payload) {
  if (payload?.page?.type === "workspace") return true;
  try {
    return new URL(getContinueUrl(payload)).pathname === "/workspace";
  } catch {
    return false;
  }
}

function isAccountProfileRequired(payload) {
  if (payload?.page?.type === "about_you") return true;
  try {
    return new URL(getContinueUrl(payload)).pathname === "/about-you";
  } catch {
    return false;
  }
}

function pickTotpFactor(payload) {
  const session = payload?.["oai-client-auth-session"] || {};
  const factors = [
    ...(Array.isArray(session.mfa_challenge_factors) ? session.mfa_challenge_factors : []),
    ...(Array.isArray(session.mfa_factors) ? session.mfa_factors : []),
  ];
  return factors.find((factor) => factor?.factor_type === "totp" && typeof factor.id === "string") || null;
}

function generateTotp(secret, timestamp = Date.now()) {
  const key = decodeBase32Secret(secret);
  const counter = BigInt(Math.floor(timestamp / 30_000));
  const counterBuffer = Buffer.alloc(8);
  counterBuffer.writeBigUInt64BE(counter);
  const digest = crypto.createHmac("sha1", key).update(counterBuffer).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const binary = ((digest[offset] & 0x7f) << 24)
    | ((digest[offset + 1] & 0xff) << 16)
    | ((digest[offset + 2] & 0xff) << 8)
    | (digest[offset + 3] & 0xff);
  return String(binary % 1_000_000).padStart(6, "0");
}

function decodeBase32Secret(value) {
  const normalized = String(value || "").toUpperCase().replace(/[\s=]/g, "");
  if (!/^[A-Z2-7]{16,128}$/.test(normalized)) {
    throw new Error("2FA key must be a Base32 string containing only A-Z and 2-7");
  }
  let bits = "";
  for (const char of normalized) {
    const index = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567".indexOf(char);
    bits += index.toString(2).padStart(5, "0");
  }
  const bytes = [];
  for (let offset = 0; offset + 8 <= bits.length; offset += 8) {
    bytes.push(Number.parseInt(bits.slice(offset, offset + 8), 2));
  }
  return Buffer.from(bytes);
}

async function authJsonStep(client, authBase, method, pathname, body, options = {}) {
  return client.getJson(method, `${authBase}${pathname}`, {
    origin: authBase,
    referer: options.referer || `${authBase}/`,
    headers: options.headers,
    json: body,
  });
}

async function continueFlow(client, payload) {
  if (isPhoneBindingRequired(payload)) throw interaction("phone_verification_required");
  if (isAccountProfileRequired(payload)) throw interaction("account_setup_required");
  if (isEmailVerificationPage(getContinueUrl(payload))) throw interaction("email_verification_required");
  const url = payload?.continue_url || payload?.page?.payload?.url;
  if (!url) return null;
  if (isLocalCallback(url)) return { finalUrl: url, last: null };
  return client.follow(url);
}

function getContinueUrl(payload) {
  return payload?.continue_url || payload?.page?.payload?.url || null;
}

function extractFirstSessionId(html) {
  const text = decodeHtml(html);
  return (
    /"session_id"\s*:\s*"(us_[^"]+)"/.exec(text)?.[1] ||
    /session_id\\",\\"(us_[^"\\]+)/.exec(text)?.[1] ||
    /name="session_id"[^>]+value="(us_[^"]+)"/.exec(text)?.[1] ||
    /value="(us_[^"]+)"[^>]+name="session_id"/.exec(text)?.[1] ||
    /\bus_[A-Za-z0-9_-]{10,}\b/.exec(text)?.[0] ||
    null
  );
}

function decodeJwtPayload(jwt) {
  const [, payload] = String(jwt).split(".");
  if (!payload) throw new Error("Invalid id_token JWT.");
  return JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
}

function pickWorkspaceId(payload) {
  const workspaces = payload?.["oai-client-auth-session"]?.workspaces;
  if (!Array.isArray(workspaces) || workspaces.length === 0) return null;
  const organization = workspaces.find((item) => item?.kind === "organization" && item?.id);
  const firstAvailable = workspaces.find((item) => item?.id);
  return (organization || firstAvailable)?.id || null;
}

function hasWorkspace(payload) {
  const workspaces = payload?.["oai-client-auth-session"]?.workspaces;
  return Array.isArray(workspaces) && workspaces.length > 0;
}

function isPhoneBindingRequired(payload) {
  return payload?.page?.type === "add_phone" || isPhoneBindingUrl(getContinueUrl(payload));
}

function isPhoneBindingUrl(value) {
  try {
    return new URL(value).pathname === "/add-phone";
  } catch {
    return false;
  }
}

function unexpectedSessionSelectionError(payload) {
  let continuePath = "<none>";
  try {
    continuePath = new URL(getContinueUrl(payload)).pathname;
  } catch {}
  const pageType = payload?.page?.type || "<none>";
  return new Error(
    `SESSION_SELECTION_INVALID: session/select returned page=${pageType}, continue_path=${continuePath}; ` +
      "the selected account session is not ready for Codex authorization",
  );
}

function assertOk(result, label) {
  const { res, text } = result;
  if (res.status >= 200 && res.status < 300) return;
  const body = text.replace(/\s+/g, " ").slice(0, 220);
  throw new Error(`${label} failed with HTTP ${res.status}: ${body}`);
}

function isRiskControlResponse(res, text) {
  const mitigated = res?.headers?.get?.("cf-mitigated") || res?.headers?.get?.("x-cf-mitigated");
  if (mitigated && /challenge/i.test(mitigated)) return true;
  const contentType = String(res?.headers?.get?.("content-type") || "");
  const body = String(text || "");
  const challengePage = /Just a moment|cdn-cgi|challenge-platform|cf-challenge/i.test(body);
  if (res?.status === 403) return /text\/html/i.test(contentType) || challengePage;
  const htmlPage = /text\/html/i.test(contentType) || /<(?:!doctype\s+html|html|head|body)\b/i.test(body);
  return [400, 409].includes(Number(res?.status)) && htmlPage && challengePage;
}

function decodeHtml(text) {
  return text
    .replace(/&quot;/g, '"')
    .replace(/&#34;/g, '"')
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">");
}

function isAuthLoginPage(value) {
  try {
    return ["/log-in", "/log-in/password", "/log-in-or-create-account"].includes(new URL(value).pathname);
  } catch {
    return false;
  }
}
export {ProtocolClient, loginChatgptWeb, verifyPassword, completeTotpMfaIfNeeded, selectChatgptLoginWorkspaceIfNeeded, continueFlow, getContinueUrl, isPasswordLoginPage, isAuthLoginPage, isMfaChallengePayload, createSentinelHeaders, authJsonStep, extractFirstSessionId, pickWorkspaceId, generateTotp, decodeJwtPayload};
