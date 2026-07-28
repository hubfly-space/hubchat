import { api, ApiError, Button, Callout, Field, Input, useMutation } from "@hubchat/shared";
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";

export default function ResetPassword() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const [error, setError] = useState<string | null>(null);
  const [mismatch, setMismatch] = useState(false);

  const reset = useMutation<{ token: string; password: string }, unknown>(
    (body) => api.post("/auth/password/reset", body),
    {
      // The reset already proved control of the mailbox and the server signs
      // the caller in on success — landing on a login form to retype the
      // password just chosen would be pure friction.
      onSuccess: () => navigate("/overview", { replace: true }),
      onError: (caught) => {
        setError(
          caught instanceof ApiError
            ? caught.message
            : "Could not reach the server. Check your connection and try again.",
        );
      },
    },
  );

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);

    const form = new FormData(event.currentTarget);
    const password = String(form.get("password") ?? "");
    const confirm = String(form.get("confirm") ?? "");

    if (password !== confirm) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    void reset.mutate({ token, password }).catch(() => {});
  };

  if (!token) {
    return (
      <>
        <h1 className="text-xl font-semibold tracking-tight text-fg">This link is incomplete</h1>
        <p className="mt-2 text-sm leading-normal text-fg-muted">
          The reset link is missing its token. Request a new one.
        </p>
        <Link to="/forgot-password" className="mt-4 inline-block text-accent-text hover:underline">
          Request a new link
        </Link>
      </>
    );
  }

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Choose a new password</h1>
        <p className="mt-1 text-sm text-fg-muted">
          Setting a new password signs out every other session on your account.
        </p>
      </header>

      {error && (
        <Callout tone="danger" className="mb-4">
          {error}
        </Callout>
      )}
      {mismatch && (
        <Callout tone="warning" className="mb-4">
          The two passwords do not match.
        </Callout>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="New password" htmlFor="password" description="At least 12 characters.">
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            inputSize="lg"
          />
        </Field>

        <Field label="Confirm new password" htmlFor="confirm">
          <Input id="confirm" name="confirm" type="password" autoComplete="new-password" required inputSize="lg" />
        </Field>

        <Callout tone="warning">
          Your existing sessions on other devices will be invalidated (§11.4 session invalidation).
        </Callout>

        <Button type="submit" variant="primary" size="lg" fullWidth loading={reset.isPending}>
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
