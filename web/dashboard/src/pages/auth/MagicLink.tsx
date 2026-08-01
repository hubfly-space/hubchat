import { ApiError, Button, Callout, clearQueryCache, api, useQuery } from "@hubchat/shared";
import { CheckCircle2, Link2 } from "lucide-react";
import { useEffect } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router-dom";

type RedeemResult =
  | { id: string; name: string; email: string }
  | { challenge: string; expires_at: string };

function safeNext(value: string | null) {
  return value && value.startsWith("/") && !value.startsWith("//") && !value.includes("\\")
    ? value
    : "/overview";
}

export default function MagicLink() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const next = safeNext(params.get("next"));
  const redeem = useQuery<RedeemResult>(
    token ? ["auth", "magic-link", token] : null,
    (signal) => apiPostMagicLink(token, signal),
    { staleTime: Infinity },
  );

  useEffect(() => {
    if (redeem.data && !("challenge" in redeem.data)) {
      clearQueryCache();
      navigate(next, { replace: true });
    }
  }, [navigate, next, redeem.data]);

  if (!token) {
    return (
      <>
        <h1 className="text-xl font-semibold tracking-tight text-fg">This link is incomplete</h1>
        <p className="mt-2 text-sm leading-normal text-fg-muted">The sign-in link is missing its token.</p>
        <Button variant="secondary" size="lg" fullWidth className="mt-6" asChild>
          <Link to="/login">Back to sign in</Link>
        </Button>
      </>
    );
  }

  if (redeem.isLoading) {
    return <p className="text-sm text-fg-muted">Signing you in securely…</p>;
  }

  if (redeem.data && "challenge" in redeem.data) {
    return <Navigate to="/two-factor" state={{ challenge: redeem.data.challenge, next }} replace />;
  }

  if (redeem.error) {
    return (
      <>
        <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
          <Link2 aria-hidden="true" className="size-5 text-accent-text" />
        </div>
        <h1 className="text-xl font-semibold tracking-tight text-fg">This sign-in link did not work</h1>
        <Callout tone="danger" className="mt-4">
          {redeem.error instanceof ApiError ? redeem.error.message : "The link is invalid or has expired."}
        </Callout>
        <Button variant="primary" size="lg" fullWidth className="mt-6" asChild>
          <Link to="/login">Request a new link</Link>
        </Button>
      </>
    );
  }

  return (
    <>
      <div className="mb-4 grid size-11 place-items-center rounded-xl border border-success-border bg-success-subtle">
        <CheckCircle2 aria-hidden="true" className="size-5 text-success-text" />
      </div>
      <p className="text-sm text-fg-muted">Your sign-in is complete. Redirecting…</p>
    </>
  );
}

function apiPostMagicLink(token: string, signal: AbortSignal) {
  return api.post<RedeemResult>("/auth/magic-link/redeem", { token }, { signal, attempts: 1 });
}
