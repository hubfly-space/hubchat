import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  type Paginated,
  Page,
  PageBody,
  PageHeader,
  Section,
  useInfinite,
  useMutation,
  formatDate,
  formatDateTime,
} from "@hubchat/shared";
import { Laptop, LogOut, Smartphone } from "lucide-react";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

type SessionInfo = {
  id: string;
  user_agent: string;
  ip: string;
  last_seen_at: string;
  created_at: string;
  expires_at: string;
  current: boolean;
};

type TrustedDeviceInfo = {
  id: string;
  name: string;
  user_agent: string;
  ip: string;
  created_at: string;
  last_used_at: string | null;
  expires_at: string;
  current: boolean;
};

/** Active session management (§11.1). */
export default function Sessions() {
  const { workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const sessions = useInfinite<SessionInfo>(["auth-sessions"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "25" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SessionInfo>>(`/auth/sessions?${params.toString()}`, { signal });
  }
  );

  const revokeOthers = useMutation<void, unknown>(
    () => api.post("/auth/sessions/revoke-others"),
    { invalidates: [["auth-sessions"]] },
  );

  const revoke = useMutation<string, unknown>(
    (sessionId) => api.delete(`/auth/sessions/${sessionId}`),
    { invalidates: [["auth-sessions"]] },
  );
  const trustedDevices = useInfinite<TrustedDeviceInfo>(["trusted-devices"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "25" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<TrustedDeviceInfo>>(`/auth/trusted-devices?${params.toString()}`, { signal });
  });
  const revokeTrusted = useMutation<string, unknown>(
    (deviceID) => api.delete(`/auth/trusted-devices/${deviceID}`),
    { invalidates: [["trusted-devices"]] },
  );
  const revokeAllTrusted = useMutation<void, unknown>(
    () => api.post("/auth/trusted-devices/revoke-all"),
    { invalidates: [["trusted-devices"]] },
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
              {sessions.isLoading ? (
                <p className="p-4 text-sm text-fg-muted">Loading sessions…</p>
              ) : sessions.error ? (
                <div className="p-4">
                  <EmptyState title="Sessions unavailable" description="Could not load your active sessions." action={<Button variant="secondary" size="sm" onClick={sessions.refetch}>Try again</Button>} />
                </div>
              ) : sessions.items.length === 0 ? (
                <div className="p-4"><EmptyState title="No active sessions" description="This account has no other active browser sessions." /></div>
              ) : (
                <>
                  <ul className="divide-y divide-line-subtle">
                    {sessions.items.map((session) => {
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
                                : `Last active ${formatDateTime(session.last_seen_at, dateFormat)}`}
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
                  {sessions.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="sm" loading={sessions.isFetching} onClick={() => void sessions.fetchNext()}>Load more sessions</Button></div>}
                </>
              )}
            </CardBody>
          </Card>
        </Section>

        <Section title="Trusted devices">
          <Card>
            <CardBody className="p-0">
              {trustedDevices.isLoading ? (
                <p className="p-4 text-sm text-fg-muted">Loading trusted devices…</p>
              ) : trustedDevices.error ? (
                <div className="p-4"><EmptyState title="Trusted devices unavailable" description="Could not load your trusted devices." action={<Button variant="secondary" size="sm" onClick={trustedDevices.refetch}>Try again</Button>} /></div>
              ) : trustedDevices.items.length ? (
                <>
                  <ul className="divide-y divide-line-subtle">
                    {trustedDevices.items.map((device) => (
                      <li key={device.id} className="flex items-center gap-3 px-4 py-3">
                        <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-inset"><Laptop aria-hidden="true" className="size-4 text-fg-muted" /></span>
                        <div className="min-w-0 flex-1">
                          <p className="flex items-center gap-2 text-sm text-fg">
                            <span className="truncate">{device.name || device.user_agent || "Trusted browser"}</span>
                            {device.current && <Badge tone="success">This device</Badge>}
                          </p>
                          <p className="mt-0.5 truncate text-xs text-fg-muted">Expires {formatDate(device.expires_at, dateFormat)} · {device.ip || "Unknown IP"}</p>
                        </div>
                        <Button variant="ghost" size="sm" loading={revokeTrusted.isPending} onClick={() => void revokeTrusted.mutate(device.id).catch(() => {})}>Revoke</Button>
                      </li>
                    ))}
                  </ul>
                  <div className="flex items-center justify-between border-t border-line-subtle p-3">
                    {trustedDevices.hasMore ? <Button variant="secondary" size="sm" loading={trustedDevices.isFetching} onClick={() => void trustedDevices.fetchNext()}>Load older devices</Button> : <span />}
                    <Button variant="danger-ghost" size="sm" loading={revokeAllTrusted.isPending} onClick={() => void revokeAllTrusted.mutate().catch(() => {})}>Revoke all trusted devices</Button>
                  </div>
                </>
              ) : (
                <div className="p-4"><EmptyState title="No trusted devices" description="Choose “Trust this device” after completing two-factor sign-in to add one." /></div>
              )}
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
