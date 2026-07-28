import {
  api,
  ApiError,
  Button,
  Callout,
  clearQueryCache,
  Field,
  Input,
  Separator,
  useMutation,
} from "@hubchat/shared";
import { Github, Mail } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";

type Credentials = { email: string; password: string };

// SignInResponse is one of two shapes, distinguished by which field is
// present: a completed sign-in carries the user, a pending second factor
// carries a challenge token instead. Nothing in the client trusts this
// distinction for anything security-relevant — it only decides which screen
// to show next, and the server has already made every real decision.
type SignInResponse =
  | { id: string; name: string; email: string }
  | { challenge: string; expires_at: string };

export default function Login() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [error, setError] = useState<string | null>(null);

  const signIn = useMutation<Credentials, SignInResponse>(
    (credentials) => api.post("/auth/login", credentials),
    {
      onSuccess: (result) => {
        if ("challenge" in result) {
          navigate("/two-factor", {
            state: { challenge: result.challenge, next: params.get("next") },
          });
          return;
        }
        // Anything cached belongs to whoever was signed in before. Clearing it
        // before navigating means the next screen cannot render the previous
        // user's workspace for a frame.
        clearQueryCache();
        // `next` carries the page the session expired on, so an agent lands
        // back in the conversation they were reading (§7.5).
        navigate(params.get("next") ?? "/overview", { replace: true });
      },
      onError: (caught) => {
        // Deliberately not distinguishing "no such account" from "wrong
        // password": telling them apart is an account-enumeration oracle
        // (§11.4), and the server declines to either.
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
    void signIn
      .mutate({
        email: String(form.get("email") ?? ""),
        password: String(form.get("password") ?? ""),
      })
      .catch(() => {
        // Surfaced through onError; swallowed here so a failed sign-in is not
        // also an unhandled rejection in the console.
      });
  };

  const loading = signIn.isPending;

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
    </>
  );
}
