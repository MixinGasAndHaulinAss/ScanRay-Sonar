// Tiny fetch wrapper with bearer-token auth and JSON convenience.
// Tokens live in localStorage so a refresh survives a page reload, with a
// short-lived access token + longer refresh token (handled by /auth/refresh).

const BASE = "/api/v1";

const ACCESS_KEY = "sonar.access";
const REFRESH_KEY = "sonar.refresh";

export interface Tokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
}

export const tokens = {
  get(): Tokens | null {
    const a = localStorage.getItem(ACCESS_KEY);
    const r = localStorage.getItem(REFRESH_KEY);
    const e = localStorage.getItem("sonar.expiresAt");
    if (!a || !r || !e) return null;
    return { accessToken: a, refreshToken: r, expiresAt: e };
  },
  set(t: Tokens) {
    localStorage.setItem(ACCESS_KEY, t.accessToken);
    localStorage.setItem(REFRESH_KEY, t.refreshToken);
    localStorage.setItem("sonar.expiresAt", t.expiresAt);
  },
  clear() {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.removeItem("sonar.expiresAt");
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

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  const t = tokens.get();
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
    if (res.status === 401) tokens.clear();
    throw new ApiError(res.status, code, msg);
  }
  return data as T;
}

export const api = {
  get: <T,>(path: string) => req<T>("GET", path),
  post: <T,>(path: string, body?: unknown) => req<T>("POST", path, body),
  put: <T,>(path: string, body?: unknown) => req<T>("PUT", path, body),
  del: <T,>(path: string) => req<T>("DELETE", path),
};
