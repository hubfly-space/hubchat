import { Button, Callout, Field, Input } from "@hubchat/shared";
import { ArrowLeft, MailCheck } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

export default function ForgotPassword() {
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <>
        <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
          <MailCheck aria-hidden="true" className="size-5 text-accent-text" />
        </div>
        <h1 className="text-xl font-semibold tracking-tight text-fg">Check your email</h1>
        <p className="mt-2 text-sm leading-normal text-fg-muted">
          If an account exists for that address, a reset link is on its way. The link expires in
          30 minutes and can be used once.
        </p>

        <Callout tone="info" className="mt-5">
          Nothing arrived? Check spam, then confirm with your administrator that outbound email is
          configured on this deployment.
        </Callout>

        <Button variant="ghost" size="sm" className="mt-6" leading={<ArrowLeft />} asChild>
          <Link to="/login">Back to sign in</Link>
        </Button>
      </>
    );
  }

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Reset your password</h1>
        <p className="mt-1 text-sm text-fg-muted">
          Enter the email on your account and we will send a reset link.
        </p>
      </header>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setSent(true);
        }}
      >
        <Field label="Email" htmlFor="email">
          <Input id="email" name="email" type="email" autoComplete="email" required inputSize="lg" />
        </Field>

        <Button type="submit" variant="primary" size="lg" fullWidth>
          Send reset link
        </Button>
      </form>

      <Button variant="ghost" size="sm" className="mt-6" leading={<ArrowLeft />} asChild>
        <Link to="/login">Back to sign in</Link>
      </Button>
    </>
  );
}
