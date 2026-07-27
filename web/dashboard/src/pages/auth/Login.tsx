import { Button, Callout, Field, Input, Separator } from "@hubchat/shared";
import { Github, Mail } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";

export default function Login() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    setLoading(true);
    setError(null);
    // The real handler posts to /api/v1/auth/login and follows the challenge
    // response — a 2FA challenge redirects to /two-factor with a scoped token.
    window.setTimeout(() => {
      setLoading(false);
      navigate("/overview");
    }, 500);
  };

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Sign in</h1>
        <p className="mt-1 text-sm text-fg-muted">Welcome back to your workspace.</p>
      </header>

      {error && (
        <Callout tone="danger" className="mb-4">
          {error}
        </Callout>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Email" htmlFor="email">
          <Input
            id="email"
            name="email"
            type="email"
            autoComplete="username"
            required
            inputSize="lg"
            placeholder="you@company.com"
          />
        </Field>

        <Field
          label="Password"
          htmlFor="password"
          hint={
            <Link to="/forgot-password" className="text-accent-text hover:underline">
              Forgot?
            </Link>
          }
        >
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            inputSize="lg"
          />
        </Field>

        <Button type="submit" variant="primary" size="lg" fullWidth loading={loading}>
          Sign in
        </Button>
      </form>

      <div className="my-6 flex items-center gap-3">
        <Separator className="flex-1" />
        <span className="text-2xs uppercase tracking-caps text-fg-muted">or</span>
        <Separator className="flex-1" />
      </div>

      <div className="flex flex-col gap-2">
        <Button variant="secondary" size="lg" fullWidth leading={<Mail />}>
          Email me a sign-in link
        </Button>
        {/* OAuth providers are adapter-configured (§11.1); the button only
            renders when the deployment has one enabled. */}
        <Button variant="secondary" size="lg" fullWidth leading={<Github />}>
          Continue with GitHub
        </Button>
      </div>

      <p className="mt-8 text-center text-xs text-fg-muted">
        No account?{" "}
        <Link to="/signup" className="text-accent-text hover:underline">
          Create one
        </Link>
      </p>

      <button
        type="button"
        onClick={() => setError("That email and password combination did not match.")}
        className="sr-only"
      >
        Simulate error
      </button>
    </>
  );
}
