import { Button, Callout, Field, Input } from "@hubchat/shared";
import { Link } from "react-router-dom";

export default function ResetPassword() {
  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Choose a new password</h1>
        <p className="mt-1 text-sm text-fg-muted">
          Setting a new password signs out every other session on your account.
        </p>
      </header>

      <form className="flex flex-col gap-4">
        <Field label="New password" htmlFor="password" description="At least 12 characters.">
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            inputSize="lg"
          />
        </Field>

        <Field label="Confirm new password" htmlFor="confirm">
          <Input id="confirm" type="password" autoComplete="new-password" required inputSize="lg" />
        </Field>

        <Callout tone="warning">
          Your existing sessions on other devices will be invalidated (§11.4 session invalidation).
        </Callout>

        <Button type="submit" variant="primary" size="lg" fullWidth>
          Set password and sign in
        </Button>
      </form>

      <p className="mt-6 text-center text-xs text-fg-muted">
        Link expired?{" "}
        <Link to="/forgot-password" className="text-accent-text hover:underline">
          Request a new one
        </Link>
      </p>
    </>
  );
}
