import { Button, Callout } from "@hubchat/shared";
import { MailCheck, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";

export default function VerifyEmail() {
  return (
    <>
      <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
        <MailCheck aria-hidden="true" className="size-5 text-accent-text" />
      </div>

      <h1 className="text-xl font-semibold tracking-tight text-fg">Verify your email</h1>
      <p className="mt-2 text-sm leading-normal text-fg-muted">
        We sent a verification link to <span className="text-fg">ada@northwind.cloud</span>.
        Open it to activate your account.
      </p>

      <Callout tone="info" className="mt-5">
        Until your email is verified you can sign in, but you cannot be assigned conversations or
        receive notifications.
      </Callout>

      <div className="mt-6 flex flex-col gap-2">
        <Button variant="secondary" size="lg" fullWidth leading={<RefreshCw />}>
          Resend verification email
        </Button>
        <Button variant="ghost" size="sm" asChild>
          <Link to="/login">Use a different account</Link>
        </Button>
      </div>
    </>
  );
}
