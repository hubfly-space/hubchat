import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  Field,
  Input,
  Select,
  Spinner,
  Switch,
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
  UserPlus,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Wordmark } from "../../components/Wordmark";

type StepId = "checks" | "migrate" | "owner" | "workspace" | "services";

const STEPS: { id: StepId; label: string; hint: string }[] = [
  { id: "checks", label: "Preflight", hint: "Configuration and database" },
  { id: "migrate", label: "Migrations", hint: "Create the schema" },
  { id: "owner", label: "Owner account", hint: "The first user" },
  { id: "workspace", label: "Workspace", hint: "Your tenant" },
  { id: "services", label: "Services", hint: "Storage, email, public URL" },
];

/**
 * First-run installation (§7.1).
 *
 * Runs before any account exists, so it is served by the Go binary at /setup
 * and is unreachable once an owner has been created. Migrations are applied
 * only after explicit approval — silently mutating a database on boot is not
 * acceptable for a self-hosted product.
 */
export default function SetupWizard() {
  const navigate = useNavigate();
  const [step, setStep] = useState<StepId>("checks");
  const [migrating, setMigrating] = useState(false);
  const [migrated, setMigrated] = useState(false);

  const index = STEPS.findIndex((item) => item.id === step);
  const advance = () => {
    const next = STEPS[index + 1];
    if (next) setStep(next.id);
    else navigate("/onboarding");
  };

  return (
    <div className="min-h-dvh bg-canvas">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-3">
          <Wordmark size="sm" />
          <Badge tone="warning">First-run setup</Badge>
        </div>
      </header>

      <div className="mx-auto grid max-w-4xl gap-8 px-6 py-10 md:grid-cols-[200px_minmax(0,1fr)]">
        {/* Step rail ------------------------------------------------------ */}
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

        {/* Step body ------------------------------------------------------ */}
        <div>
          {step === "checks" && (
            <StepShell
              title="Preflight checks"
              description="Hubchat verified its configuration before starting. Everything below must pass before the schema is created."
              onNext={advance}
              nextLabel="Continue"
            >
              <div className="flex flex-col gap-2">
                <CheckRow
                  icon={<Database />}
                  label="PostgreSQL connection"
                  detail="postgres://hubchat@localhost:5432/hubchat · server 17.2"
                  state="pass"
                />
                <CheckRow
                  icon={<Server />}
                  label="Public URL"
                  detail="https://support.northwind.cloud"
                  state="pass"
                />
                <CheckRow
                  icon={<HardDrive />}
                  label="Data directory"
                  detail="/var/lib/hubchat · writable · 214 GB free"
                  state="pass"
                />
                <CheckRow
                  icon={<Mail />}
                  label="Outbound email"
                  detail="No SMTP configured — customer notifications will queue"
                  state="warn"
                />
                <CheckRow
                  icon={<ShieldCheck />}
                  label="Secret key"
                  detail="HUBCHAT_SECRET_KEY present · 32 bytes"
                  state="pass"
                />
              </div>

              <Callout tone="warning" className="mt-4">
                Email is optional for installation but required before customers can receive ticket
                notifications. You can configure it now or later in Settings.
              </Callout>
            </StepShell>
          )}

          {step === "migrate" && (
            <StepShell
              title="Create the database schema"
              description="Hubchat ships its migrations inside the binary. Nothing has been written to your database yet."
              onNext={advance}
              nextDisabled={!migrated}
              nextLabel="Continue"
            >
              <Card variant="sunken">
                <CardBody className="flex items-center justify-between gap-4">
                  <div>
                    <p className="text-sm text-fg">42 pending migrations</p>
                    <p className="mt-0.5 text-xs text-fg-muted">
                      0001_accounts → 0042_analytics_rollups
                    </p>
                  </div>
                  {migrated ? (
                    <Badge tone="success" leading={<Check />}>
                      Applied
                    </Badge>
                  ) : (
                    <Button
                      variant="primary"
                      size="sm"
                      loading={migrating}
                      onClick={() => {
                        setMigrating(true);
                        window.setTimeout(() => {
                          setMigrating(false);
                          setMigrated(true);
                        }, 1200);
                      }}
                    >
                      Apply migrations
                    </Button>
                  )}
                </CardBody>
              </Card>

              <p className="mt-4 text-xs text-fg-muted">
                Prefer to run this yourself, or automating the install?
              </p>
              <CodeBlock
                className="mt-2"
                language="bash"
                code={"hubchat migrate\nhubchat migrate status --json"}
              />
            </StepShell>
          )}

          {step === "owner" && (
            <StepShell
              title="Create the owner account"
              description="The owner holds every capability, including destructive workspace operations. You can add more members later."
              onNext={advance}
              nextLabel="Create account"
            >
              <div className="flex flex-col gap-4">
                <Field label="Full name" htmlFor="owner-name">
                  <Input id="owner-name" inputSize="lg" defaultValue="Ada Mwangi" />
                </Field>
                <Field label="Email" htmlFor="owner-email">
                  <Input
                    id="owner-email"
                    type="email"
                    inputSize="lg"
                    defaultValue="ada@northwind.cloud"
                  />
                </Field>
                <Field
                  label="Password"
                  htmlFor="owner-password"
                  description="At least 12 characters. This account can delete the workspace — choose accordingly."
                >
                  <Input id="owner-password" type="password" inputSize="lg" />
                </Field>
                <Switch
                  label="Require two-factor authentication for this account"
                  description="Recommended. You will set it up immediately after signing in."
                  defaultChecked
                />
              </div>
            </StepShell>
          )}

          {step === "workspace" && (
            <StepShell
              title="Create your first workspace"
              description="A workspace is the tenant boundary. Every customer, conversation, and setting belongs to exactly one."
              onNext={advance}
              nextLabel="Create workspace"
            >
              <div className="flex flex-col gap-4">
                <Field label="Workspace name" htmlFor="ws-name">
                  <Input id="ws-name" inputSize="lg" defaultValue="Northwind Cloud" />
                </Field>
                <Field
                  label="Slug"
                  htmlFor="ws-slug"
                  description="Used in portal URLs and API scoping. Hard to change later."
                >
                  <Input id="ws-slug" inputSize="lg" defaultValue="northwind" suffix=".hubchat.app" />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Timezone" htmlFor="ws-tz">
                    <Select
                      id="ws-tz"
                      size="lg"
                      defaultValue="Europe/Lisbon"
                      options={[
                        { value: "Europe/Lisbon", label: "Europe/Lisbon (WEST)" },
                        { value: "Europe/Berlin", label: "Europe/Berlin (CEST)" },
                        { value: "America/New_York", label: "America/New_York (EDT)" },
                        { value: "Asia/Singapore", label: "Asia/Singapore (+08)" },
                      ]}
                    />
                  </Field>
                  <Field label="Ticket prefix" htmlFor="ws-prefix">
                    <Input id="ws-prefix" inputSize="lg" defaultValue="SUP" mono />
                  </Field>
                </div>
              </div>
            </StepShell>
          )}

          {step === "services" && (
            <StepShell
              title="Optional services"
              description="Hubchat runs without any of these. Configure them now or from Settings once you are in."
              onNext={advance}
              nextLabel="Finish setup"
            >
              <div className="flex flex-col gap-3">
                <ServiceCard
                  icon={<Mail />}
                  title="Outbound email"
                  status="not-configured"
                  detail="Required for ticket notifications, magic links, and password resets."
                />
                <ServiceCard
                  icon={<HardDrive />}
                  title="Object storage"
                  status="local"
                  detail="Attachments are stored on local disk at /var/lib/hubchat/files. Switch to S3-compatible storage for multi-node deployments."
                />
                <ServiceCard
                  icon={<ShieldCheck />}
                  title="TLS"
                  status="ok"
                  detail="Terminated by your reverse proxy. Secure cookies are enabled."
                />
              </div>
            </StepShell>
          )}
        </div>
      </div>
    </div>
  );
}

function StepShell({
  title,
  description,
  children,
  onNext,
  nextLabel,
  nextDisabled,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
  onNext: () => void;
  nextLabel: string;
  nextDisabled?: boolean;
}) {
  return (
    <div>
      <h1 className="text-xl font-semibold tracking-tight text-fg">{title}</h1>
      <p className="mt-1.5 max-w-measure text-sm leading-normal text-fg-muted">{description}</p>
      <div className="mt-6">{children}</div>
      <div className="mt-8 flex justify-end">
        <Button variant="primary" size="lg" onClick={onNext} disabled={nextDisabled}>
          {nextLabel}
        </Button>
      </div>
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
  state: "pass" | "warn" | "fail" | "pending";
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
        {state === "pending" && <Spinner />}
      </span>
    </div>
  );
}

function ServiceCard({
  icon,
  title,
  detail,
  status,
}: {
  icon: React.ReactNode;
  title: string;
  detail: string;
  status: "ok" | "local" | "not-configured";
}) {
  return (
    <Card>
      <CardBody className="flex items-start gap-3">
        <span className="mt-0.5 shrink-0 text-fg-muted [&_svg]:size-4">{icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium text-fg">{title}</p>
            <Badge
              tone={status === "ok" ? "success" : status === "local" ? "neutral" : "warning"}
            >
              {status === "ok" ? "Ready" : status === "local" ? "Local disk" : "Not configured"}
            </Badge>
          </div>
          <p className="mt-1 text-xs leading-normal text-fg-muted">{detail}</p>
        </div>
        <Button variant="secondary" size="sm" leading={<UserPlus />}>
          Configure
        </Button>
      </CardBody>
    </Card>
  );
}
