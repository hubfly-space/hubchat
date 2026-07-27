import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Page,
  PageBody,
  PageHeader,
  Section,
  formatDateTime,
  formatRelativeShort,
} from "@hubchat/shared";
import { Laptop, LogOut, Smartphone, Terminal } from "lucide-react";
import { NOW } from "../../data/fixtures";

const SESSIONS = [
  {
    id: "ses_1",
    device: "MacBook Pro · Chrome 141",
    location: "Lisbon, Portugal",
    ip: "203.0.113.44",
    lastActive: "2026-07-26T14:19:00Z",
    current: true,
    kind: "desktop" as const,
  },
  {
    id: "ses_2",
    device: "iPhone · Safari 18",
    location: "Lisbon, Portugal",
    ip: "203.0.113.91",
    lastActive: "2026-07-26T08:04:00Z",
    current: false,
    kind: "mobile" as const,
  },
  {
    id: "ses_3",
    device: "Linux · Firefox 142",
    location: "Frankfurt, Germany",
    ip: "198.51.100.17",
    lastActive: "2026-07-22T21:40:00Z",
    current: false,
    kind: "desktop" as const,
  },
];

const API_SESSIONS = [
  { id: "cli_1", label: "hubchat CLI", detail: "Issued on 2026-06-30 · expires 2026-09-28", ip: "203.0.113.44" },
];

/** Active session management (§11.1). */
export default function Sessions() {
  return (
    <Page>
      <PageHeader
        title="Sessions"
        description="Every device currently signed in to your account."
        actions={
          <Button variant="danger" size="sm" leading={<LogOut />}>
            Sign out everywhere else
          </Button>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-5">
          Do not recognise something here? Sign it out, then change your password — that invalidates
          every other session immediately.
        </Callout>

        <Section title="Browser sessions">
          <Card>
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {SESSIONS.map((session) => {
                  const Icon = session.kind === "mobile" ? Smartphone : Laptop;
                  return (
                    <li key={session.id} className="flex items-center gap-3 px-4 py-3">
                      <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-inset">
                        <Icon aria-hidden="true" className="size-4 text-fg-muted" />
                      </span>

                      <div className="min-w-0 flex-1">
                        <p className="flex items-center gap-2 text-sm text-fg">
                          <span className="truncate">{session.device}</span>
                          {session.current && <Badge tone="success">This device</Badge>}
                        </p>
                        <p className="mt-0.5 truncate text-xs text-fg-muted">
                          {session.location} · {session.ip}
                        </p>
                        <p className="text-2xs text-fg-disabled">
                          {session.current
                            ? "Active now"
                            : `Last active ${formatRelativeShort(session.lastActive, NOW)} ago · ${formatDateTime(session.lastActive)}`}
                        </p>
                      </div>

                      {!session.current && (
                        <Button variant="ghost" size="sm">
                          Sign out
                        </Button>
                      )}
                    </li>
                  );
                })}
              </ul>
            </CardBody>
          </Card>
        </Section>

        <Section title="Command-line sessions">
          <Card>
            <CardHeader
              title="CLI tokens"
              description="Issued by hubchat login. Scoped to your capabilities and expire independently of browser sessions."
            />
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {API_SESSIONS.map((session) => (
                  <li key={session.id} className="flex items-center gap-3 px-4 py-3">
                    <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-inset">
                      <Terminal aria-hidden="true" className="size-4 text-fg-muted" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm text-fg">{session.label}</p>
                      <p className="mt-0.5 truncate text-xs text-fg-muted">{session.detail}</p>
                    </div>
                    <Button variant="ghost" size="sm">
                      Revoke
                    </Button>
                  </li>
                ))}
              </ul>
            </CardBody>
          </Card>
        </Section>

        <Section title="Recent sign-in activity">
          <Card>
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {[
                  { at: "2026-07-26T07:02:00Z", result: "Success", detail: "Password + 2FA · Lisbon" },
                  { at: "2026-07-25T06:58:00Z", result: "Success", detail: "Magic link · Lisbon" },
                  { at: "2026-07-24T23:11:00Z", result: "Failed", detail: "Wrong password · Frankfurt" },
                ].map((entry) => (
                  <li key={entry.at} className="flex items-center gap-3 px-4 py-2.5">
                    <Badge tone={entry.result === "Success" ? "neutral" : "danger"}>
                      {entry.result}
                    </Badge>
                    <span className="min-w-0 flex-1 truncate text-xs text-fg-secondary">
                      {entry.detail}
                    </span>
                    <span className="shrink-0 text-2xs tabular text-fg-muted">
                      {formatDateTime(entry.at)}
                    </span>
                  </li>
                ))}
              </ul>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
