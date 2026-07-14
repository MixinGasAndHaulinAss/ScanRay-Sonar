// DocsRedirect mints an HttpOnly docs session cookie, then leaves the SPA
// for the embedded MkDocs site at /docs/.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";

export default function DocsRedirect() {
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await api.post<void>("/docs/session");
        if (!cancelled) {
          window.location.assign("/docs/");
        }
      } catch (e) {
        if (cancelled) return;
        setError(
          e instanceof ApiError ? e.message : "Could not open documentation",
        );
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) {
    return (
      <div className="space-y-3 rounded-xl border border-red-800/60 bg-red-950/40 p-4 text-sm text-red-200">
        <p>{error}</p>
        <Link to="/" className="text-sonar-400 hover:underline">
          Back to dashboard
        </Link>
      </div>
    );
  }

  return <div className="text-sm text-slate-400">Opening documentation…</div>;
}
