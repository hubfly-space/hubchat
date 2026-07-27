import { Button, Callout, Field, Input, Separator } from "@hubchat/shared";
import { ArrowLeft, MailCheck } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { portal } from "../data";

/**
 * Customer sign-in.
 *
 * Magic link leads because it is the right default for a support portal: the
 * customer has no reason to maintain a password with you, and a password they
 * reuse is a liability you inherit.
 */
export default function SignIn() {
  const [sent, setSent] = useState(false);

  return (
    <div
      data-branded
      style={{ ["--hc-accent-brand" as string]: portal.accent }}
      className="flex min-h-dvh flex-col items-center justify-center bg-canvas px-4 py-12"
    >
      <div className="w-full max-w-sm">
        <Link to="/" className="mb-8 flex items-center justify-center gap-2">
          <span
            aria-hidden="true"
            className="grid size-7 place-items-center rounded-md text-xs font-bold text-white"
            style={{ backgroundColor: portal.accent }}
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

            <form
              className="flex flex-col gap-4"
              onSubmit={(event) => {
                event.preventDefault();
                setSent(true);
              }}
            >
              <Field label="Email">
                <Input type="email" inputSize="lg" autoComplete="email" required autoFocus />
              </Field>

              <Button type="submit" variant="primary" size="lg" fullWidth>
                Email me a sign-in link
              </Button>
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
