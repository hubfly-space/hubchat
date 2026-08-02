import { ApiError, Button, Callout, Field, Input, Separator, api, useMutation } from "@hubchat/shared";
import { ArrowLeft, MailCheck } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { portalAccent, portalErrorMessage, safePortalNext, usePortal } from "../portal-context";
import { portalText } from "../i18n";

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
  const t = (key: string, fallback: string) => portalText(data, key, fallback);
  const next = safePortalNext(params.get("next"), params.get("portal"));
  const deleted = params.get("deleted") === "1";
  const requestLink = useMutation(({ email: value, portal, next: destination }: { email: string; portal?: string; next?: string }) =>
    api.post("/portal/auth/magic-link", { email: value, portal, next: destination }),
  );

  useEffect(() => {
    const token = params.get("token");
    if (!token) return;
    void api.post("/portal/auth/magic-link/redeem", { token }).then(() => navigate(next, { replace: true }));
  }, [navigate, next, params]);

  if (isLoading) return <div className="grid min-h-dvh place-items-center bg-canvas text-sm text-fg-muted">{t("loading_portal", "Loading portal…")}</div>;
  if (error || !data) return <div className="grid min-h-dvh place-items-center bg-canvas px-4 text-center text-sm text-fg-muted">{portalErrorMessage(error)}</div>;

  const { portal } = data;
  const accent = portalAccent(portal);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    void requestLink.mutate({ email, portal: portal.id, next }).then(() => setSent(true));
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
            <h1 className="text-xl font-semibold tracking-tight text-fg">{t("check_email", "Check your email")}</h1>
            <p className="mt-2 text-sm leading-normal text-fg-muted">
              {t("sent_sign_in", "We sent a sign-in link to your address. It works once and expires in 15 minutes.")}
            </p>
            <Button variant="ghost" size="sm" className="mt-6" leading={<ArrowLeft />} onClick={() => setSent(false)}>
              {t("different_email", "Use a different email")}
            </Button>
          </div>
        ) : (
          <>
            <header className="mb-6 text-center">
              <h1 className="text-xl font-semibold tracking-tight text-fg">{t("sign_in", "Sign in")}</h1>
              <p className="mt-1.5 text-sm text-fg-muted">
                {t("sign_in_subtitle", "To view your requests and follow feedback.")}
              </p>
            </header>
            {deleted && <Callout tone="info" className="mb-4">{t("anonymised", "Your profile has been anonymised and all portal sessions were revoked.")}</Callout>}

            <form
              className="flex flex-col gap-4"
              onSubmit={submit}
            >
              <Field label={t("email", "Email")}>
                <Input type="email" inputSize="lg" autoComplete="email" required autoFocus value={email} onChange={(event) => setEmail(event.target.value)} />
              </Field>

              <Button type="submit" variant="primary" size="lg" fullWidth>
                {requestLink.isPending ? t("sending", "Sending…") : t("email_sign_in", "Email me a sign-in link")}
              </Button>
              {Boolean(requestLink.error) && <p className="text-sm text-danger">{requestLink.error instanceof ApiError ? requestLink.error.message : t("could_not_send", "Could not send the sign-in link.")}</p>}
            </form>

            <div className="my-6 flex items-center gap-3">
              <Separator className="flex-1" />
              <span className="text-2xs uppercase tracking-caps text-fg-muted">{t("or", "or")}</span>
              <Separator className="flex-1" />
            </div>

            <Callout tone="info">
              {t("no_account", "You do not need an account to send a request —")} {" "}
              <Link to="/tickets/new" className="underline">
                {t("submit_here", "submit one here")}
              </Link>{" "}
              {t("reply_by_email", "and we will reply by email.")}
            </Callout>
          </>
        )}
      </div>
    </div>
  );
}
