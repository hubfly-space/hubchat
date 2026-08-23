import {
  api,
  ApiError,
  Button,
  Callout,
  clearQueryCache,
  Field,
  Input,
  Progress,
  useMutation,
} from "@hubchat/shared";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

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

type Step = "account" | "workspace";

export default function Signup() {
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>("account");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const strength = useMemo(() => scorePassword(password), [password]);

  const createAccount = useMutation<{ name: string; email: string; password: string }, unknown>(
    (body) => api.post("/auth/signup", body),
    {
      onSuccess: () => setStep("workspace"),
      onError: (caught) => setError(describeError(caught)),
    },
  );

  const createWorkspace = useMutation<{ name: string; slug: string }, unknown>(
    (body) => api.post("/workspaces", body),
    {
      onSuccess: () => {
        clearQueryCache();
        navigate("/overview", { replace: true });
      },
      onError: (caught) => setError(describeError(caught)),
    },
  );

  if (step === "workspace") {
    return <WorkspaceStep error={error} onSubmit={(body) => void createWorkspace.mutate(body)} loading={createWorkspace.isPending} />;
  }

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    const form = new FormData(event.currentTarget);
    void createAccount.mutate({
      name: String(form.get("name") ?? ""),
      email: String(form.get("email") ?? ""),
      password: String(form.get("password") ?? ""),
    });
  };

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

      {error && (
        <Callout tone="danger" className="mb-4">
          {error}
        </Callout>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4">
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

        <Button type="submit" variant="primary" size="lg" fullWidth loading={createAccount.isPending}>
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

function WorkspaceStep({
  error,
  onSubmit,
  loading,
}: {
  error: string | null;
  onSubmit: (body: { name: string; slug: string }) => void;
  loading: boolean;
}) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit({ name, slug });
  };

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">Name your workspace</h1>
        <p className="mt-1 text-sm text-fg-muted">
          A workspace is the tenant boundary. Every customer, conversation, and setting belongs to
          exactly one.
        </p>
      </header>

      {error && (
        <Callout tone="danger" className="mb-4">
          {error}
        </Callout>
      )}

      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Workspace name" htmlFor="ws-name">
          <Input
            id="ws-name"
            inputSize="lg"
            required
            value={name}
            onChange={(event) => {
              const value = event.target.value;
              setName(value);
              if (!slugTouched) setSlug(slugify(value));
            }}
          />
        </Field>
        <Field
          label="Slug"
          htmlFor="ws-slug"
          description="Used in portal URLs and API scoping. Lowercase letters, numbers, and hyphens."
        >
          <Input
            id="ws-slug"
            inputSize="lg"
            required
            pattern="[a-z0-9](?:[a-z0-9]|-){1,38}[a-z0-9]"
            value={slug}
            onChange={(event) => {
              setSlugTouched(true);
              setSlug(event.target.value);
            }}
          />
        </Field>

        <Button type="submit" variant="primary" size="lg" fullWidth loading={loading}>
          Create workspace
        </Button>
      </form>
    </>
  );
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

function describeError(caught: unknown): string {
  return caught instanceof ApiError
    ? caught.message
    : "Could not reach the server. Check your connection and try again.";
}
