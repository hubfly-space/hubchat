import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Field,
  Input,
  QueryError,
  useMutation,
  useQuery,
  cn,
} from "@hubchat/shared";
import {
  AlertTriangle,
  Check,
  Database,
  HardDrive,
  Mail,
  Server,
  ShieldCheck,
} from "lucide-react";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { Wordmark } from "../../components/Wordmark";

type StepId = "checks" | "owner" | "workspace";

const STEPS: { id: StepId; label: string; hint: string }[] = [
  { id: "checks", label: "Preflight", hint: "Configuration and database" },
  { id: "owner", label: "Owner account", hint: "The first user" },
  { id: "workspace", label: "Workspace", hint: "Your tenant" },
];

type SetupState = {
  installed: boolean;
  public_url: string;
  secret_key_ok: boolean;
  email_configured: boolean;
  storage_backend: string;
  migrations_total: number;
  migrations_applied: number;
};

/**
 * First-run installation (§7.1).
 *
 * Reachable at any time, but only useful before an owner exists — once
 * `/v1/setup/state` reports `installed`, this redirects to sign-in rather than
 * let a second visitor create a competing "first" owner. Schema migrations are
 * never triggered from here: they already ran (or were verified clean) before
 * this server accepted its first request, per `HUBCHAT_MIGRATE` — silently
 * mutating a database from a browser click is not a self-hosted product's
 * business.
 */
export default function SetupWizard() {
  const navigate = useNavigate();
  const [step, setStep] = useState<StepId>("checks");

  const state = useQuery<SetupState>(["setup", "state"], (signal) =>
    api.get<SetupState>("/setup/state", { signal, fresh: true }),
  );

  const index = STEPS.findIndex((item) => item.id === step);
  const advance = () => {
    const next = STEPS[index + 1];
    if (next) setStep(next.id);
    else navigate("/onboarding");
  };

  if (state.isLoading) {
    return (
      <div className="grid min-h-dvh place-items-center bg-canvas">
        <p className="text-sm text-fg-muted">Checking installation state…</p>
      </div>
    );
  }

  if (state.error) {
    return (
      <div className="mx-auto max-w-md pt-24">
        <QueryError error={state.error} retry={state.refetch} />
      </div>
    );
  }

  // An owner already exists. Whoever is here either finished setup already or
  // followed a stale link — either way, sign-in is the correct next step, not
  // a wizard that would try to create a second first-run owner.
  if (state.data?.installed) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="min-h-dvh bg-canvas">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-3">
          <Wordmark size="sm" />
          <Badge tone="warning">First-run setup</Badge>
        </div>
      </header>

      <div className="mx-auto grid max-w-4xl gap-8 px-6 py-10 md:grid-cols-[200px_minmax(0,1fr)]">
        <ol className="relative hidden md:block">
          <span aria-hidden="true" className="absolute bottom-3 left-[11px] top-3 w-px bg-line" />
          {STEPS.map((item, itemIndex) => {
            const done = itemIndex < index;
            const active = itemIndex === index;
            return (
              <li key={item.id} className="relative mb-5 flex gap-3 last:mb-0">
                <span
                  className={cn(
                    "z-10 grid size-6 shrink-0 place-items-center rounded-full border text-2xs font-semibold",
                    done && "border-success bg-success text-fg-inverse",
                    active && "border-accent bg-accent text-accent-fg",
                    !done && !active && "border-line bg-surface text-fg-muted",
                  )}
                >
                  {done ? <Check className="size-3" strokeWidth={3} /> : itemIndex + 1}
                </span>
                <span className="min-w-0 pt-0.5">
                  <span
                    className={cn(
                      "block text-xs font-medium",
                      active ? "text-fg" : done ? "text-fg-secondary" : "text-fg-muted",
                    )}
                  >
                    {item.label}
                  </span>
                  <span className="block text-2xs text-fg-muted">{item.hint}</span>
                </span>
              </li>
            );
          })}
        </ol>

        <div>
          {step === "checks" && state.data && <ChecksStep state={state.data} onNext={advance} />}
          {step === "owner" && <OwnerStep onNext={advance} />}
          {step === "workspace" && <WorkspaceStep onNext={advance} />}
        </div>
      </div>
    </div>
  );
}

function ChecksStep({ state, onNext }: { state: SetupState; onNext: () => void }) {
  const migrationsCurrent = state.migrations_applied >= state.migrations_total;

  return (
    <StepShell
      title="Preflight checks"
      description="Hubchat verified its configuration before starting. Everything below reflects the server you are actually talking to — not a simulation."
      onNext={onNext}
      nextLabel="Continue"
    >
      <div className="flex flex-col gap-2">
        <CheckRow
          icon={<Database />}
          label="Database schema"
          detail={`${state.migrations_applied} of ${state.migrations_total} migrations applied`}
          state={migrationsCurrent ? "pass" : "fail"}
        />
        <CheckRow
          icon={<Server />}
          label="Public URL"
          detail={state.public_url || "Not configured"}
          state={state.public_url ? "pass" : "fail"}
        />
        <CheckRow
          icon={<HardDrive />}
          label="Attachment storage"
          detail={state.storage_backend === "s3" ? "S3-compatible storage" : "Local disk"}
          state="pass"
        />
        <CheckRow
          icon={<Mail />}
          label="Outbound email"
          detail={
            state.email_configured
              ? "SMTP configured"
              : "No SMTP configured — notifications will queue"
          }
          state={state.email_configured ? "pass" : "warn"}
        />
        <CheckRow
          icon={<ShieldCheck />}
          label="Secret key"
          detail={state.secret_key_ok ? "Present, at least 32 bytes" : "Missing or too short"}
          state={state.secret_key_ok ? "pass" : "fail"}
        />
      </div>

      {!state.email_configured && (
        <Callout tone="warning" className="mt-4">
          Email is optional for installation but required before customers can receive ticket
          notifications. You can configure it now with <code>HUBCHAT_SMTP_HOST</code> or later.
        </Callout>
      )}
    </StepShell>
  );
}

function OwnerStep({ onNext }: { onNext: () => void }) {
  const [error, setError] = useState<string | null>(null);

  const createOwner = useMutation<
    { name: string; email: string; password: string },
    unknown
  >((body) => api.post("/auth/signup", body), {
    onSuccess: onNext,
    onError: (caught) => {
      setError(
        caught instanceof ApiError
          ? caught.message
          : "Could not reach the server. Check your connection and try again.",
      );
    },
  });

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    const form = new FormData(event.currentTarget);
    void createOwner
      .mutate({
        name: String(form.get("name") ?? ""),
        email: String(form.get("email") ?? ""),
        password: String(form.get("password") ?? ""),
      })
      .catch(() => {});
  };

  return (
    <StepShell
      title="Create the owner account"
      description="The owner holds every capability, including destructive workspace operations. You can add more members later."
      onNext={() => {}}
      nextLabel="Create account"
      hideNext
    >
      <form id="owner-form" onSubmit={submit} className="flex flex-col gap-4">
        {error && <Callout tone="danger">{error}</Callout>}

        <Field label="Full name" htmlFor="owner-name">
          <Input id="owner-name" name="name" inputSize="lg" required autoComplete="name" />
        </Field>
        <Field label="Email" htmlFor="owner-email">
          <Input
            id="owner-email"
            name="email"
            type="email"
            inputSize="lg"
            required
            autoComplete="username"
          />
        </Field>
        <Field
          label="Password"
          htmlFor="owner-password"
          description="At least 12 characters. This account can delete the workspace — choose accordingly."
        >
          <Input
            id="owner-password"
            name="password"
            type="password"
            inputSize="lg"
            required
            minLength={12}
            autoComplete="new-password"
          />
        </Field>
      </form>

      <div className="mt-8 flex justify-end">
        <Button
          type="submit"
          form="owner-form"
          variant="primary"
          size="lg"
          loading={createOwner.isPending}
        >
          Create account
        </Button>
      </div>
    </StepShell>
  );
}

function WorkspaceStep({ onNext }: { onNext: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  // Tracks whether the person has typed into the slug field directly, so
  // auto-deriving it from the name never clobbers a deliberate edit.
  const [slugTouched, setSlugTouched] = useState(false);

  const createWorkspace = useMutation<{ name: string; slug: string }, unknown>(
    (body) => api.post("/workspaces", body),
    {
      onSuccess: onNext,
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
    void createWorkspace
      .mutate({
        name: String(form.get("name") ?? ""),
        slug: String(form.get("slug") ?? ""),
      })
      .catch(() => {});
  };

  return (
    <StepShell
      title="Create your first workspace"
      description="A workspace is the tenant boundary. Every customer, conversation, and setting belongs to exactly one."
      onNext={() => {}}
      nextLabel="Create workspace"
      hideNext
    >
      <form id="workspace-form" onSubmit={submit} className="flex flex-col gap-4">
        {error && <Callout tone="danger">{error}</Callout>}

        <Field label="Workspace name" htmlFor="ws-name">
          <Input
            id="ws-name"
            name="name"
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
            name="slug"
            inputSize="lg"
            required
            pattern="[a-z0-9][a-z0-9-]{1,38}[a-z0-9]"
            value={slug}
            onChange={(event) => {
              setSlugTouched(true);
              setSlug(event.target.value);
            }}
          />
        </Field>
      </form>

      <div className="mt-8 flex justify-end">
        <Button
          type="submit"
          form="workspace-form"
          variant="primary"
          size="lg"
          loading={createWorkspace.isPending}
        >
          Create workspace
        </Button>
      </div>
    </StepShell>
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

function StepShell({
  title,
  description,
  children,
  onNext,
  nextLabel,
  nextDisabled,
  hideNext,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
  onNext: () => void;
  nextLabel: string;
  nextDisabled?: boolean;
  hideNext?: boolean;
}) {
  return (
    <div>
      <h1 className="text-xl font-semibold tracking-tight text-fg">{title}</h1>
      <p className="mt-1.5 max-w-measure text-sm leading-normal text-fg-muted">{description}</p>
      <div className="mt-6">{children}</div>
      {!hideNext && (
        <div className="mt-8 flex justify-end">
          <Button variant="primary" size="lg" onClick={onNext} disabled={nextDisabled}>
            {nextLabel}
          </Button>
        </div>
      )}
    </div>
  );
}

function CheckRow({
  icon,
  label,
  detail,
  state,
}: {
  icon: React.ReactNode;
  label: string;
  detail: string;
  state: "pass" | "warn" | "fail";
}) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-line bg-surface px-3 py-2.5">
      <span className="mt-0.5 shrink-0 text-fg-muted [&_svg]:size-4">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm text-fg">{label}</span>
        <span className="block text-xs text-fg-muted">{detail}</span>
      </span>
      <span className="mt-0.5 shrink-0">
        {state === "pass" && <Check className="size-4 text-success-text" />}
        {state === "warn" && <AlertTriangle className="size-4 text-warning-text" />}
        {state === "fail" && <AlertTriangle className="size-4 text-danger-text" />}
      </span>
    </div>
  );
}
