import { api, ApiError, Button, Callout, useMutation, useQuery } from "@hubchat/shared";
import { CheckCircle2, MailCheck, RefreshCw } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";

type Me = { workspace: { id: string }; viewer: { email: string } } | undefined;

export default function VerifyEmail() {
  const [params] = useSearchParams();
  const token = params.get("token");

  // A token in the URL means this page was opened from the email link — try
  // to redeem it immediately rather than waiting for a click. Without a
  // token, this is just the "we sent you a link" holding screen.
  const verify = useQuery(
    token ? ["verify-email", token] : null,
    () => api.post<{ id: string }>("/auth/verify-email", { token }),
    { staleTime: Infinity },
  );

  const resend = useMutation<void, unknown>(() => api.post("/auth/verify-email/resend"));

  // Best-effort: shows the address being verified when a session exists.
  // Not required for the redeem flow itself, which only needs the token.
  const bootstrap = useQuery<Me>(["bootstrap-address-hint"], (signal) =>
    api.get<Me>("/bootstrap", { signal, fresh: false }).catch(() => undefined),
  );

  if (token) {
    if (verify.isLoading) {
      return <p className="text-sm text-fg-muted">Verifying your email…</p>;
    }
    if (verify.isSuccess) {
      return (
        <>
          <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
            <CheckCircle2 aria-hidden="true" className="size-5 text-success-text" />
          </div>
          <h1 className="text-xl font-semibold tracking-tight text-fg">Email verified</h1>
          <p className="mt-2 text-sm leading-normal text-fg-muted">
            Your address is confirmed. You can now be assigned conversations and receive
            notifications.
          </p>
          <Button variant="primary" size="lg" fullWidth className="mt-6" asChild>
            <Link to="/overview">Continue</Link>
          </Button>
        </>
      );
    }
    if (verify.error) {
      return (
        <>
          <h1 className="text-xl font-semibold tracking-tight text-fg">This link did not work</h1>
          <p className="mt-2 text-sm leading-normal text-fg-muted">
            {verify.error instanceof ApiError
              ? verify.error.message
              : "The link is invalid or has expired."}
          </p>
          <Button variant="secondary" size="lg" fullWidth className="mt-6" onClick={() => void resend.mutate()}>
            Send a new link
          </Button>
        </>
      );
    }
  }

  return (
    <>
      <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
        <MailCheck aria-hidden="true" className="size-5 text-accent-text" />
      </div>

      <h1 className="text-xl font-semibold tracking-tight text-fg">Verify your email</h1>
      <p className="mt-2 text-sm leading-normal text-fg-muted">
        We sent a verification link to{" "}
        <span className="text-fg">{bootstrap.data?.viewer.email ?? "your address"}</span>. Open it
        to activate your account.
      </p>

      <Callout tone="info" className="mt-5">
        Until your email is verified you can sign in, but you cannot be assigned conversations or
        receive notifications.
      </Callout>

      {resend.isSuccess && (
        <Callout tone="success" className="mt-4">
          Sent. Check your inbox.
        </Callout>
      )}
      {resend.error && (
        <Callout tone="danger" className="mt-4">
          {resend.error instanceof ApiError ? resend.error.message : "Could not send that email."}
        </Callout>
      )}

      <div className="mt-6 flex flex-col gap-2">
        <Button
          variant="secondary"
          size="lg"
          fullWidth
          leading={<RefreshCw />}
          loading={resend.isPending}
          onClick={() => void resend.mutate()}
        >
          Resend verification email
        </Button>
        <Button variant="ghost" size="sm" asChild>
          <Link to="/login">Use a different account</Link>
        </Button>
      </div>
    </>
  );
}
