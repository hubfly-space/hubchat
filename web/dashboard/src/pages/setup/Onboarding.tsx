import {
  Badge,
  Button,
  Card,
  CardBody,
  CodeBlock,
  Field,
  Input,
  RadioGroup,
  cn,
} from "@hubchat/shared";
import { ArrowRight, Check, Copy, Mail, MessageSquare, Radio, Ticket } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Wordmark } from "../../components/Wordmark";

type Step = "usecase" | "inbox" | "surface" | "install" | "invite";

/**
 * Workspace onboarding (§7.2) — distinct from installation.
 *
 * Optimised for one outcome: a working widget receiving a real test message
 * within a few minutes. Everything not on that path (SLAs, custom fields,
 * automations) is deliberately absent; those screens exist and are discoverable
 * later, and putting them here would double the time to first value.
 */
export default function Onboarding() {
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>("usecase");
  const [useCase, setUseCase] = useState("support");
  const [installed, setInstalled] = useState(false);

  const order: Step[] = ["usecase", "inbox", "surface", "install", "invite"];
  const index = order.indexOf(step);

  const next = () => {
    const following = order[index + 1];
    if (following) setStep(following);
    else navigate("/overview");
  };

  return (
    <div className="min-h-dvh bg-canvas">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-3xl items-center justify-between px-6 py-3">
          <Wordmark size="sm" />
          <div className="flex items-center gap-3">
            <span className="text-xs tabular text-fg-muted">
              Step {index + 1} of {order.length}
            </span>
            <Button variant="ghost" size="sm" onClick={() => navigate("/overview")}>
              Skip setup
            </Button>
          </div>
        </div>
        <div className="h-0.5 bg-chart-track">
          <div
            className="h-full bg-accent transition-[width] duration-slow ease-out"
            style={{ width: `${((index + 1) / order.length) * 100}%` }}
          />
        </div>
      </header>

      <main className="mx-auto max-w-3xl px-6 py-12">
        {step === "usecase" && (
          <Shell
            title="What will you use Hubchat for first?"
            description="This only pre-selects sensible defaults. Nothing here is permanent."
            onNext={next}
          >
            <RadioGroup
              variant="cards"
              value={useCase}
              onValueChange={setUseCase}
              aria-label="Primary use case"
              options={[
                {
                  value: "support",
                  label: "Live chat and support",
                  description: "A widget on your product, a shared inbox behind it.",
                },
                {
                  value: "tickets",
                  label: "Structured ticketing",
                  description: "Forms, custom fields, SLAs, and a customer portal.",
                },
                {
                  value: "feedback",
                  label: "Product feedback and roadmap",
                  description: "Public boards, voting, and a changelog.",
                },
                {
                  value: "selfserve",
                  label: "Self-service help centre",
                  description: "A knowledge base with contact as the fallback.",
                },
              ]}
            />
          </Shell>
        )}

        {step === "inbox" && (
          <Shell
            title="Name your first inbox"
            description="An inbox is where conversations land. Most teams start with one and split later by product or department."
            onNext={next}
          >
            <div className="flex flex-col gap-4">
              <Field label="Inbox name" htmlFor="inbox-name">
                <Input id="inbox-name" inputSize="lg" defaultValue="Support" />
              </Field>
              <Field
                label="Reply-from address"
                htmlFor="inbox-email"
                description="Customers see this on email notifications."
              >
                <Input
                  id="inbox-email"
                  inputSize="lg"
                  defaultValue="support"
                  suffix="@northwind.cloud"
                />
              </Field>
            </div>
          </Shell>
        )}

        {step === "surface" && (
          <Shell
            title="Choose how customers reach you"
            description="You can add every other channel later — this is just what gets set up now."
            onNext={next}
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <SurfaceCard
                icon={<MessageSquare />}
                title="Website widget"
                detail="A launcher on your app or marketing site. Live chat, help articles, and ticket forms."
                selected
              />
              <SurfaceCard
                icon={<Radio />}
                title="Customer portal"
                detail="A hosted, branded site where customers track tickets and browse guides."
              />
              <SurfaceCard
                icon={<Mail />}
                title="Email"
                detail="Forward an existing support address into this inbox."
              />
              <SurfaceCard
                icon={<Ticket />}
                title="Embedded form"
                detail="A standalone form you can drop into any page."
              />
            </div>
          </Shell>
        )}

        {step === "install" && (
          <Shell
            title="Install the widget"
            description="Drop this before the closing </body> tag. It loads asynchronously and never blocks your page."
            onNext={next}
            nextLabel={installed ? "Continue" : "I'll do this later"}
          >
            <CodeBlock
              filename="index.html"
              code={`<script>
  (function(h,u,b){h.Hubchat=h.Hubchat||function(){(h.Hubchat.q=h.Hubchat.q||[]).push(arguments)};
  var s=u.createElement('script');s.async=1;s.src=b;u.head.appendChild(s)})
  (window,document,'https://support.northwind.cloud/widget/v1.js');

  Hubchat('boot', { key: 'pk_live_8f2a41cd9b7e' });
</script>`}
            />

            <Card variant="sunken" className="mt-4">
              <CardBody className="flex items-center gap-3">
                <span
                  className={cn(
                    "grid size-8 shrink-0 place-items-center rounded-full",
                    installed ? "bg-success-subtle text-success-text" : "bg-fill text-fg-muted",
                  )}
                >
                  {installed ? (
                    <Check className="size-4" strokeWidth={3} />
                  ) : (
                    <span className="size-2 animate-pulse rounded-full bg-current" />
                  )}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-fg">
                    {installed ? "Widget detected" : "Waiting for the first widget load…"}
                  </p>
                  <p className="text-xs text-fg-muted">
                    {installed
                      ? "Received a bootstrap request from app.northwind.cloud."
                      : "This updates automatically once your site loads the script."}
                  </p>
                </div>
                {!installed && (
                  <Button variant="secondary" size="sm" onClick={() => setInstalled(true)}>
                    Simulate
                  </Button>
                )}
              </CardBody>
            </Card>

            <div className="mt-4 flex flex-wrap gap-2">
              <Button variant="ghost" size="sm" leading={<Copy />}>
                Email these instructions to a developer
              </Button>
              <Button variant="ghost" size="sm">
                npm package instead
              </Button>
            </div>
          </Shell>
        )}

        {step === "invite" && (
          <Shell
            title="Invite your team"
            description="Add the people who will answer conversations. You can change roles at any time."
            onNext={next}
            nextLabel="Finish"
          >
            <div className="flex flex-col gap-2">
              {["", "", ""].map((_, itemIndex) => (
                <div key={itemIndex} className="flex gap-2">
                  <Input
                    inputSize="lg"
                    type="email"
                    placeholder="teammate@northwind.cloud"
                    aria-label={`Invite email ${itemIndex + 1}`}
                  />
                  <div className="w-36 shrink-0">
                    <Input inputSize="lg" defaultValue="Agent" readOnly aria-label="Role" />
                  </div>
                </div>
              ))}
            </div>
            <Button variant="ghost" size="sm" className="mt-2">
              Add another
            </Button>
          </Shell>
        )}
      </main>
    </div>
  );
}

function Shell({
  title,
  description,
  children,
  onNext,
  nextLabel = "Continue",
}: {
  title: string;
  description: string;
  children: React.ReactNode;
  onNext: () => void;
  nextLabel?: string;
}) {
  return (
    <div className="animate-fade-up">
      <h1 className="text-2xl font-semibold tracking-tighter text-fg">{title}</h1>
      <p className="mt-2 max-w-measure text-sm leading-normal text-fg-muted">{description}</p>
      <div className="mt-8">{children}</div>
      <div className="mt-10 flex justify-end">
        <Button variant="primary" size="lg" onClick={onNext} trailing={<ArrowRight />}>
          {nextLabel}
        </Button>
      </div>
    </div>
  );
}

function SurfaceCard({
  icon,
  title,
  detail,
  selected,
}: {
  icon: React.ReactNode;
  title: string;
  detail: string;
  selected?: boolean;
}) {
  return (
    <Card
      interactive
      className={cn("p-4", selected && "border-accent-border bg-accent-subtle")}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-fg-muted [&_svg]:size-4">{icon}</span>
        {selected && <Badge tone="accent">Selected</Badge>}
      </div>
      <p className="mt-2.5 text-sm font-medium text-fg">{title}</p>
      <p className="mt-1 text-xs leading-normal text-fg-muted">{detail}</p>
    </Card>
  );
}
