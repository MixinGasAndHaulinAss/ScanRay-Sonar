// Tiny fetch wrapper with bearer-token auth and JSON convenience.
// Tokens live in localStorage so a refresh survives a page reload, with a
// short-lived access token + longer refresh token (handled by /auth/refresh).
//
// On 401 the client transparently calls /auth/refresh once and retries the
// original request. Only if the refresh ALSO fails do we clear tokens and
// kick the user back to the login screen. Refresh is serialized via a
// singleton in-flight promise so 50 simultaneous 401s on a tab wake-up
// only fire one refresh.

const BASE = "/api/v1";

const ACCESS_KEY = "sonar.access";
const REFRESH_KEY = "sonar.refresh";
const EXPIRES_KEY = "sonar.expiresAt";

export interface Tokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
}

export const tokens = {
  get(): Tokens | null {
    const a = localStorage.getItem(ACCESS_KEY);
    const r = localStorage.getItem(REFRESH_KEY);
    const e = localStorage.getItem(EXPIRES_KEY);
    if (!a || !r || !e) return null;
    return { accessToken: a, refreshToken: r, expiresAt: e };
  },
  set(t: Tokens) {
    localStorage.setItem(ACCESS_KEY, t.accessToken);
    localStorage.setItem(REFRESH_KEY, t.refreshToken);
    localStorage.setItem(EXPIRES_KEY, t.expiresAt);
  },
  clear() {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.removeItem(EXPIRES_KEY);
  },
};

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

interface RefreshResponse {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
}

// In-flight refresh promise. While a refresh is happening, every other
// request that needs a fresh token awaits the same promise instead of
// stampeding the /auth/refresh endpoint.
let refreshing: Promise<Tokens | null> | null = null;

async function performRefresh(): Promise<Tokens | null> {
  const t = tokens.get();
  if (!t || !t.refreshToken) return null;
  try {
    const res = await fetch(`${BASE}/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refreshToken: t.refreshToken }),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as RefreshResponse;
    if (!data.accessToken || !data.refreshToken || !data.expiresAt) return null;
    const next: Tokens = {
      accessToken: data.accessToken,
      refreshToken: data.refreshToken,
      expiresAt: data.expiresAt,
    };
    tokens.set(next);
    return next;
  } catch {
    return null;
  }
}

async function refreshAccessToken(): Promise<Tokens | null> {
  if (!refreshing) {
    refreshing = performRefresh().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

// isExpired returns true when the cached access token is within `skewMs`
// of expiry. We refresh proactively when this fires so the user never
// sees the 401 + retry round-trip in the network panel.
function isExpired(t: Tokens, skewMs = 30_000): boolean {
  const exp = Date.parse(t.expiresAt);
  if (Number.isNaN(exp)) return false;
  return Date.now() + skewMs >= exp;
}

async function req<T>(
  method: string,
  path: string,
  body?: unknown,
  retried = false,
): Promise<T> {
  // Proactive refresh: if we're holding an access token that's about to
  // expire (or already has), refresh BEFORE issuing the request so the
  // wire never sees a 401 from us. Skips for /auth/* so we don't loop.
  let t = tokens.get();
  if (t && !path.startsWith("/auth/") && isExpired(t)) {
    const next = await refreshAccessToken();
    if (next) t = next;
  }

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (t) headers["Authorization"] = `Bearer ${t.accessToken}`;

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body == null ? undefined : JSON.stringify(body),
  });

  if (res.status === 204) return undefined as T;

  const ct = res.headers.get("content-type") ?? "";
  const isJSON = ct.includes("application/json");
  const data = isJSON ? await res.json().catch(() => ({})) : await res.text();

  if (!res.ok) {
    const code = (isJSON && (data as any).code) || "error";
    const msg = (isJSON && (data as any).message) || res.statusText || "request failed";

    // Reactive refresh: server says the access token is bad. If we
    // haven't tried yet AND we have a refresh token, swap in a new
    // access token and retry once. Only on a second 401 do we clear
    // session state and let the redirect to /login happen.
    if (
      res.status === 401 &&
      !retried &&
      !path.startsWith("/auth/") &&
      tokens.get()?.refreshToken
    ) {
      const next = await refreshAccessToken();
      if (next) {
        return req<T>(method, path, body, true);
      }
      tokens.clear();
    } else if (res.status === 401) {
      tokens.clear();
    }

    throw new ApiError(res.status, code, msg);
  }
  return data as T;
}

export const api = {
  get: <T,>(path: string) => req<T>("GET", path),
  post: <T,>(path: string, body?: unknown) => req<T>("POST", path, body),
  put: <T,>(path: string, body?: unknown) => req<T>("PUT", path, body),
  patch: <T,>(path: string, body?: unknown) => req<T>("PATCH", path, body),
  del: <T,>(path: string) => req<T>("DELETE", path),
};
