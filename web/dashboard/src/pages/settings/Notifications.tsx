import { ApiError, Button, Callout, Card, CardBody, EmptyState, Page, PageBody, PageHeader, Section, api, idempotencyKey, useMutation, useQuery } from "@hubchat/shared";
import { Bell } from "lucide-react";
import { useEffect, useState } from "react";

type Preference = { type: string; in_app: boolean; email: boolean; browser: boolean; sound: boolean };
type PreferenceInput = Preference;

const EVENTS = [
  { type: "assignment", label: "A conversation is assigned to me", in_app: true, email: true, browser: true, sound: false },
  { type: "mention", label: "Someone mentions me in a note", in_app: true, email: true, browser: true, sound: false },
  { type: "reply", label: "A customer replies to a thread I own", in_app: true, email: false, browser: true, sound: false },
  { type: "sla_warning", label: "An SLA I own is approaching breach", in_app: true, email: false, browser: true, sound: false },
  { type: "sla_breach", label: "An SLA I own has breached", in_app: true, email: true, browser: true, sound: false },
  { type: "team_unassigned", label: "Unassigned work arrives in my team’s inbox", in_app: true, email: false, browser: false, sound: false },
  { type: "feedback", label: "Feedback is submitted on a board I moderate", in_app: true, email: false, browser: false, sound: false },
] as const;

/** Live per-member notification preferences (§6.15). */
export default function Notifications() {
  const query = useQuery<{ data: Preference[] }>(["notification-preferences"], (signal) => api.get("/notifications/preferences", { signal }));
  const [draft, setDraft] = useState<Record<string, PreferenceInput>>({});
  const [browserPermission, setBrowserPermission] = useState<NotificationPermission | "unsupported">("default");
  useEffect(() => {
    setBrowserPermission("Notification" in window ? window.Notification.permission : "unsupported");
  }, []);
  useEffect(() => {
    if (!query.data) return;
    const saved = new Map(query.data.data.map((item) => [item.type, item]));
    setDraft(Object.fromEntries(EVENTS.map((event) => [event.type, { ...event, ...(saved.get(event.type) ?? {}) }])));
  }, [query.data]);
  const save = useMutation<{ data: PreferenceInput[] }, { data: Preference[] }>((input) => api.put("/notifications/preferences", input, { idempotencyKey: idempotencyKey() }), { invalidates: [["notification-preferences"]] });
  const update = (type: string, channel: keyof Omit<Preference, "type">, value: boolean) => setDraft((current) => { const existing = current[type] ?? { type, in_app: false, email: false, browser: false, sound: false }; return { ...current, [type]: { ...existing, [channel]: value } }; });

  return <Page>
    <PageHeader title="Notifications" description="Choose how your workspace activity reaches you. Preferences are stored per member." actions={<Button variant="primary" size="sm" loading={save.isPending} disabled={query.isLoading || Object.keys(draft).length === 0} onClick={() => void save.mutate({ data: Object.values(draft) }).catch(() => {})}>Save changes</Button>} />
    <PageBody width="narrow">
      {query.isLoading ? <p className="text-sm text-fg-muted">Loading notification preferences…</p> : query.error ? <EmptyState icon={Bell} title="Notification preferences unavailable" description={query.error instanceof ApiError ? query.error.message : "Could not load your notification preferences."} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : <>
        <Section title="Agent notifications"><Card><CardBody className="p-0"><div className="overflow-x-auto"><table className="w-full min-w-[600px] text-sm"><thead><tr className="border-b border-line"><th className="px-4 py-2 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">Event</th>{["In-app", "Email", "Browser", "Sound"].map((channel) => <th key={channel} className="w-20 px-2 py-2 text-center text-2xs font-semibold uppercase tracking-caps text-fg-muted">{channel}</th>)}</tr></thead><tbody>{EVENTS.map((event) => { const value = draft[event.type] ?? event; return <tr key={event.type} className="border-b border-line-subtle last:border-b-0"><td className="px-4 py-2.5 text-fg">{event.label}</td>{(["in_app", "email", "browser", "sound"] as const).map((channel) => <td key={channel} className="px-2 py-2.5 text-center"><input type="checkbox" checked={Boolean(value[channel])} onChange={(input) => update(event.type, channel, input.target.checked)} aria-label={`${event.label} via ${channel}`} className="size-4 accent-accent" /></td>)}</tr>; })}</tbody></table></div></CardBody></Card></Section>
        <Section title="Delivery notes"><Callout tone="info" icon={<Bell />}>In-app notifications are durable and appear in the dashboard bell. Email-enabled alerts are queued through the configured SMTP worker. Browser alerts appear while this dashboard is open when your browser allows them.</Callout><div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-line bg-inset px-3 py-2.5"><div><p className="text-xs font-medium text-fg">Browser permission</p><p className="mt-0.5 text-2xs text-fg-muted">{browserPermission === "granted" ? "Allowed" : browserPermission === "denied" ? "Blocked in browser settings" : browserPermission === "unsupported" ? "Not supported by this browser" : "Not requested"}</p></div>{browserPermission === "default" && <Button variant="secondary" size="sm" onClick={() => void window.Notification.requestPermission().then(setBrowserPermission)}>Allow browser alerts</Button>}</div>{Boolean(save.error) && <p className="mt-3 text-sm text-danger">Could not save notification preferences. Your current selections are still on this page.</p>}{save.isSuccess && <p className="mt-3 text-sm text-success-text">Notification preferences saved.</p>}</Section>
      </>}
    </PageBody>
  </Page>;
}
