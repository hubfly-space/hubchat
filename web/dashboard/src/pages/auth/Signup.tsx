import { Button, Callout, Field, Input, Progress } from "@hubchat/shared";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";

/** Length-dominant scoring — it is the only factor that reliably matters. */
function scorePassword(value: string): { score: number; label: string } {
  if (!value) return { score: 0, label: "" };
  let score = Math.min(value.length / 16, 1) * 0.7;
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) score += 0.1;
  if (/\d/.test(value)) score += 0.1;
  if (/[^\w\s]/.test(value)) score += 0.1;
  score = Math.min(score, 1);

  return {
    score,
    label: score < 0.4 ? "Too short" : score < 0.7 ? "Reasonable" : "Strong",
  };
}

export default function Signup() {
  const [password, setPassword] = useState("");
  const strength = useMemo(() => scorePassword(password), [password]);

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Create your account</h1>
        <p className="mt-1 text-sm text-fg-muted">
          You will create or join a workspace in the next step.
        </p>
      </header>

      <Callout tone="info" className="mb-5">
        This deployment allows open sign-up. An administrator can restrict it to invitations
        only in Settings → Security.
      </Callout>

      <form className="flex flex-col gap-4">
        <Field label="Full name" htmlFor="name">
          <Input id="name" name="name" autoComplete="name" required inputSize="lg" />
        </Field>

        <Field label="Work email" htmlFor="email">
          <Input id="email" name="email" type="email" autoComplete="email" required inputSize="lg" />
        </Field>

        <Field
          label="Password"
          htmlFor="password"
          description="At least 12 characters. A passphrase beats a short complex string."
        >
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            inputSize="lg"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          {password && (
            <div className="mt-2 flex items-center gap-2">
              <Progress
                value={strength.score}
                size="xs"
                tone={strength.score < 0.4 ? "danger" : strength.score < 0.7 ? "warning" : "success"}
                label="Password strength"
              />
              <span className="shrink-0 text-2xs text-fg-muted">{strength.label}</span>
            </div>
          )}
        </Field>

        <Button type="submit" variant="primary" size="lg" fullWidth>
          Create account
        </Button>
      </form>

      <p className="mt-6 text-center text-xs leading-normal text-fg-muted">
        By creating an account you agree to this deployment's terms, set by its administrator.
      </p>

      <p className="mt-4 text-center text-xs text-fg-muted">
        Already have an account?{" "}
        <Link to="/login" className="text-accent-text hover:underline">
          Sign in
        </Link>
      </p>
    </>
  );
}
