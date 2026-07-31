import { ApiError, Button, Callout, Field, Input, Separator, api, useMutation } from "@hubchat/shared";
import { ArrowLeft, MailCheck } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { portalAccent, portalErrorMessage, usePortal } from "../portal-context";

/**
 * Customer sign-in.
 *
 * Magic link leads because it is the right default for a support portal: the
 * customer has no reason to maintain a password with you, and a password they
 * reuse is a liability you inherit.
 */
export default function SignIn() {
  const [sent, setSent] = useState(false);
  const [email, setEmail] = useState("");
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { data, isLoading, error } = usePortal();
  const deleted = params.get("deleted") === "1";
  const requestLink = useMutation(({ email: value, portal }: { email: string; portal?: string }) =>
    api.post("/portal/auth/magic-link", { email: value, portal }),
  );

  useEffect(() => {
    const token = params.get("token");
    if (!token) return;
    void api.post("/portal/auth/magic-link/redeem", { token }).then(() => navigate(`/?portal=${encodeURIComponent(params.get("portal") ?? "")}`, { replace: true }));
  }, [navigate, params]);

  if (isLoading) return <div className="grid min-h-dvh place-items-center bg-canvas text-sm text-fg-muted">Loading portal…</div>;
  if (error || !data) return <div className="grid min-h-dvh place-items-center bg-canvas px-4 text-center text-sm text-fg-muted">{portalErrorMessage(error)}</div>;

  const { portal } = data;
  const accent = portalAccent(portal);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    void requestLink.mutate({ email, portal: portal.id }).then(() => setSent(true));
  };

  return (
    <div
      data-branded
      style={{ ["--hc-accent-brand" as string]: accent }}
      className="flex min-h-dvh flex-col items-center justify-center bg-canvas px-4 py-12"
    >
      <div className="w-full max-w-sm">
        <Link to="/" className="mb-8 flex items-center justify-center gap-2">
          <span
            aria-hidden="true"
            className="grid size-7 place-items-center rounded-md text-xs font-bold text-white"
            style={{ backgroundColor: accent }}
          >
            N
          </span>
          <span className="text-md font-semibold tracking-tight text-fg">{portal.name}</span>
        </Link>

        {sent ? (
          <div className="text-center">
            <div className="mx-auto mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
              <MailCheck aria-hidden="true" className="size-5 text-accent-text" />
            </div>
            <h1 className="text-xl font-semibold tracking-tight text-fg">Check your email</h1>
            <p className="mt-2 text-sm leading-normal text-fg-muted">
              We sent a sign-in link to your address. It works once and expires in 15 minutes.
            </p>
            <Button variant="ghost" size="sm" className="mt-6" leading={<ArrowLeft />} onClick={() => setSent(false)}>
              Use a different email
            </Button>
          </div>
        ) : (
          <>
            <header className="mb-6 text-center">
              <h1 className="text-xl font-semibold tracking-tight text-fg">Sign in</h1>
              <p className="mt-1.5 text-sm text-fg-muted">
                To view your requests and follow feedback.
              </p>
            </header>
            {deleted && <Callout tone="info" className="mb-4">Your profile has been anonymised and all portal sessions were revoked.</Callout>}

            <form
              className="flex flex-col gap-4"
              onSubmit={submit}
            >
              <Field label="Email">
                <Input type="email" inputSize="lg" autoComplete="email" required autoFocus value={email} onChange={(event) => setEmail(event.target.value)} />
              </Field>

              <Button type="submit" variant="primary" size="lg" fullWidth>
                {requestLink.isPending ? "Sending…" : "Email me a sign-in link"}
              </Button>
              {Boolean(requestLink.error) && <p className="text-sm text-danger">{requestLink.error instanceof ApiError ? requestLink.error.message : "Could not send the sign-in link."}</p>}
            </form>

            <div className="my-6 flex items-center gap-3">
              <Separator className="flex-1" />
              <span className="text-2xs uppercase tracking-caps text-fg-muted">or</span>
              <Separator className="flex-1" />
            </div>

            <Callout tone="info">
              You do not need an account to send a request —{" "}
              <Link to="/tickets/new" className="underline">
                submit one here
              </Link>{" "}
              and we will reply by email.
            </Callout>
          </>
        )}
      </div>
    </div>
  );
}
