import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { Laptop, LogOut, Smartphone } from "lucide-react";

type SessionInfo = {
  id: string;
  user_agent: string;
  ip: string;
  last_seen_at: string;
  created_at: string;
  expires_at: string;
  current: boolean;
};

/** Active session management (§11.1). */
export default function Sessions() {
  const sessions = useQuery<{ data: SessionInfo[] }>(["auth-sessions"], (signal) =>
    api.get("/auth/sessions", { signal }),
  );

  const revokeOthers = useMutation<void, unknown>(
    () => api.post("/auth/sessions/revoke-others"),
    { invalidates: [["auth-sessions"]] },
  );

  const revoke = useMutation<string, unknown>(
    (sessionId) => api.delete(`/auth/sessions/${sessionId}`),
    { invalidates: [["auth-sessions"]] },
  );

  return (
    <Page>
      <PageHeader
        title="Sessions"
        description="Every device currently signed in to your account."
        actions={
          <Button
            variant="danger"
            size="sm"
            leading={<LogOut />}
            loading={revokeOthers.isPending}
            onClick={() => void revokeOthers.mutate().catch(() => {})}
          >
            Sign out everywhere else
          </Button>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-5">
          Do not recognise something here? Sign it out, then change your password — that invalidates
          every other session immediately.
        </Callout>

        {revoke.error ? (
          <Callout tone="danger" className="mb-4">
            {revoke.error instanceof ApiError ? revoke.error.message : "Could not sign out that session."}
          </Callout>
        ) : null}
        {revokeOthers.error ? (
          <Callout tone="danger" className="mb-4">
            {revokeOthers.error instanceof ApiError
              ? revokeOthers.error.message
              : "Could not sign out your other sessions."}
          </Callout>
        ) : null}

        <Section title="Browser sessions">
          <Card>
            <CardBody className="p-0">
              <QueryBoundary query={sessions}>
                {({ data }) => (
                  <ul className="divide-y divide-line-subtle">
                    {data.map((session) => {
                      const Icon = /mobile|iphone|android/i.test(session.user_agent)
                        ? Smartphone
                        : Laptop;
                      return (
                        <li key={session.id} className="flex items-center gap-3 px-4 py-3">
                          <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-inset">
                            <Icon aria-hidden="true" className="size-4 text-fg-muted" />
                          </span>

                          <div className="min-w-0 flex-1">
                            <p className="flex items-center gap-2 text-sm text-fg">
                              <span className="truncate">{session.user_agent || "Unknown device"}</span>
                              {session.current && <Badge tone="success">This device</Badge>}
                            </p>
                            <p className="mt-0.5 truncate text-xs text-fg-muted">{session.ip}</p>
                            <p className="text-2xs text-fg-disabled">
                              {session.current
                                ? "Active now"
                                : `Last active ${new Date(session.last_seen_at).toLocaleString()}`}
                            </p>
                          </div>

                          {!session.current && (
                            <Button
                              variant="ghost"
                              size="sm"
                              loading={revoke.isPending}
                              onClick={() => void revoke.mutate(session.id).catch(() => {})}
                            >
                              Sign out
                            </Button>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </QueryBoundary>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
