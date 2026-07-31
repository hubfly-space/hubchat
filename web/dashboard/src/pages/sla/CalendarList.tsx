import {
  ApiError,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  Dialog,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Switch,
  api,
  cn,
  idempotencyKey,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { CalendarClock, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";

type Window = { start: string; end: string };
type Holiday = { id: string; date: string; name: string };
type Calendar = {
  id: string;
  name: string;
  timezone: string;
  weekly: Window[][];
  holidays: Holiday[];
  is_default: boolean;
  updated_at?: string;
};
type CalendarInput = Omit<Calendar, "id" | "updated_at" | "holidays"> & { holidays: Array<{ date: string; name: string }> };

const DAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

const defaultWeekly = (): Window[][] =>
  Array.from({ length: 7 }, (_, index) => (index < 5 ? [{ start: "09:00", end: "17:00" }] : []));

export default function CalendarList() {
  const query = useQuery<{ data: Calendar[] }>(["sla-calendars"], (signal) => api.get("/sla/calendars", { signal }));
  const calendars = query.data?.data ?? [];
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const active = calendars.find((calendar) => calendar.id === selectedID) ?? calendars[0];
  const [draft, setDraft] = useState<Calendar | null>(null);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [timezone, setTimezone] = useState("UTC");
  const [holidayDate, setHolidayDate] = useState("");
  const [holidayName, setHolidayName] = useState("");

  useEffect(() => {
    if (!selectedID && active) setSelectedID(active.id);
  }, [active, selectedID]);

  useEffect(() => {
    setDraft(active ? { ...active, weekly: active.weekly.map((windows) => windows.map((window) => ({ ...window }))) } : null);
  }, [active]);

  const create = useMutation<CalendarInput, Calendar>(
    (input) => api.post("/sla/calendars", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["sla-calendars"]],
      onSuccess: (value) => {
        setOpen(false);
        setName("");
        setTimezone("UTC");
        setSelectedID(value.id);
      },
    },
  );
  const save = useMutation<CalendarInput, Calendar>(
    (input) => api.patch(`/sla/calendars/${encodeURIComponent(active?.id ?? "")}`, input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["sla-calendars"]],
      onSuccess: (value) => setDraft(value),
    },
  );

  const updateDraft = (updater: (current: Calendar) => Calendar) => setDraft((current) => (current ? updater(current) : current));
  const updateWindow = (day: number, index: number, field: keyof Window, value: string) => {
    updateDraft((current) => ({
      ...current,
      weekly: current.weekly.map((windows, dayIndex) =>
        dayIndex === day ? windows.map((window, windowIndex) => windowIndex === index ? { ...window, [field]: value } : window) : windows,
      ),
    }));
  };

  const saveCalendar = () => {
    if (!draft || !active) return;
    void save.mutate({
      name: draft.name.trim(),
      timezone: draft.timezone.trim(),
      weekly: draft.weekly,
      holidays: draft.holidays.map(({ date, name: holiday }) => ({ date, name: holiday })),
      is_default: draft.is_default,
    }).catch(() => {});
  };

  const addHoliday = () => {
    if (!draft || !holidayDate || !holidayName.trim()) return;
    updateDraft((current) => ({
      ...current,
      holidays: [...current.holidays, { id: `new-${Date.now()}`, date: holidayDate, name: holidayName.trim() }].sort((a, b) => a.date.localeCompare(b.date)),
    }));
    setHolidayDate("");
    setHolidayName("");
  };

  return (
    <Page>
      <PageHeader
        title="Business hours"
        description="Calendars used by SLA timers, widget availability, and time-based rules."
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New calendar</Button></DialogTrigger>
            <DialogContent
              title="Create business-hours calendar"
              footer={<><Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()} onClick={() => void create.mutate({ name: name.trim(), timezone: timezone.trim() || "UTC", weekly: defaultWeekly(), holidays: [], is_default: calendars.length === 0 }).catch(() => {})}>Create calendar</Button></>}
            >
              <div className="space-y-4">
                <Field label="Name"><Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="Kigali support hours" /></Field>
                <Field label="Timezone" description="Use an IANA timezone such as Africa/Kigali."><Input mono value={timezone} onChange={(event) => setTimezone(event.target.value)} placeholder="Africa/Kigali" /></Field>
                {Boolean(create.error) && <p className="text-sm text-danger">{create.error instanceof ApiError ? create.error.message : "Could not create calendar."}</p>}
              </div>
            </DialogContent>
          </Dialog>
        }
      />
      <PageBody>
        {query.isLoading ? <p className="text-sm text-fg-muted">Loading calendars…</p> : query.error ? <EmptyState icon={CalendarClock} title="Calendars unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : !active || !draft ? <EmptyState icon={CalendarClock} title="No business-hours calendars" description="Create a calendar before attaching an SLA policy." /> : (
          <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
            <nav aria-label="Calendars" className="space-y-1">
              {calendars.map((calendar) => <button key={calendar.id} type="button" onClick={() => setSelectedID(calendar.id)} className={cn("flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm", calendar.id === active.id ? "bg-accent-subtle" : "hover:bg-fill")}><CalendarClock className="size-3.5 shrink-0 text-fg-muted" /><span className="min-w-0 flex-1 truncate">{calendar.name}</span>{calendar.is_default && <Badge tone="neutral">Default</Badge>}</button>)}
            </nav>
            <div className="min-w-0">
              <div className="mb-5 flex flex-wrap items-end gap-3">
                <Field label="Calendar name" className="min-w-56 flex-1"><Input value={draft.name} onChange={(event) => updateDraft((current) => ({ ...current, name: event.target.value }))} /></Field>
                <Field label="Timezone" description="IANA timezone"><Input mono value={draft.timezone} onChange={(event) => updateDraft((current) => ({ ...current, timezone: event.target.value }))} /></Field>
                <label className="flex h-9 items-center gap-2 pb-1 text-sm text-fg-secondary"><Switch checked={draft.is_default} onCheckedChange={(checked) => updateDraft((current) => ({ ...current, is_default: checked }))} aria-label="Set as default calendar" /> Default</label>
                <Button variant="primary" size="sm" leading={<Save />} loading={save.isPending} onClick={saveCalendar}>Save changes</Button>
              </div>
              {Boolean(save.error) && <p className="mb-4 text-sm text-danger">{save.error instanceof ApiError ? save.error.message : "Could not save this calendar."}</p>}
              <Section title="Weekly schedule" description="Add one or more working windows per day. Empty days are closed.">
                <Card><CardHeader title={draft.name} description={`All times in ${draft.timezone}.`} /><CardBody className="p-0"><ul className="divide-y divide-line-subtle">{DAYS.map((day, dayIndex) => { const windows = draft.weekly[dayIndex] ?? []; return <li key={day} className="flex flex-wrap items-start gap-4 px-4 py-3"><span className={cn("w-24 shrink-0 pt-2 text-sm", windows.length ? "text-fg" : "text-fg-disabled")}>{day}</span><div className="min-w-0 flex-1 space-y-2">{windows.length ? windows.map((window, windowIndex) => <div key={`${day}-${windowIndex}`} className="flex flex-wrap items-center gap-2"><Input inputSize="sm" type="time" value={window.start} onChange={(event) => updateWindow(dayIndex, windowIndex, "start", event.target.value)} aria-label={`${day} window ${windowIndex + 1} start`} /><span className="text-xs text-fg-muted">to</span><Input inputSize="sm" type="time" value={window.end === "24:00" ? "23:59" : window.end} onChange={(event) => updateWindow(dayIndex, windowIndex, "end", event.target.value)} aria-label={`${day} window ${windowIndex + 1} end`} /><Button variant="ghost" size="sm" iconOnly aria-label={`Remove ${day} window ${windowIndex + 1}`} leading={<Trash2 />} onClick={() => updateDraft((current) => ({ ...current, weekly: current.weekly.map((dayWindows, index) => index === dayIndex ? dayWindows.filter((_, index) => index !== windowIndex) : dayWindows) }))} /></div>) : <span className="pt-2 text-xs text-fg-muted">Closed</span>}<Button variant="ghost" size="sm" leading={<Plus />} onClick={() => updateDraft((current) => ({ ...current, weekly: current.weekly.map((dayWindows, index) => index === dayIndex ? [...dayWindows, { start: "09:00", end: "17:00" }] : dayWindows) }))}>Add window</Button></div></li>; })}</ul></CardBody></Card>
              </Section>
              <Section title="Holidays" description="Full days when timers pause in this calendar's timezone.">
                <Card><CardBody className="space-y-3"><div className="flex flex-wrap items-end gap-2"><Field label="Date"><Input type="date" value={holidayDate} onChange={(event) => setHolidayDate(event.target.value)} /></Field><Field label="Name" className="min-w-48 flex-1"><Input value={holidayName} onChange={(event) => setHolidayName(event.target.value)} placeholder="Public holiday" /></Field><Button variant="secondary" size="sm" leading={<Plus />} disabled={!holidayDate || !holidayName.trim()} onClick={addHoliday}>Add holiday</Button></div>{draft.holidays.length ? <ul className="divide-y divide-line-subtle rounded-md border border-line">{draft.holidays.map((holiday) => <li key={holiday.id} className="flex items-center gap-3 px-3 py-2.5"><span className="font-mono text-xs tabular text-fg-muted">{holiday.date}</span><span className="min-w-0 flex-1 text-sm text-fg">{holiday.name}</span><Button variant="ghost" size="sm" iconOnly aria-label={`Remove ${holiday.name}`} leading={<Trash2 />} onClick={() => updateDraft((current) => ({ ...current, holidays: current.holidays.filter((item) => item.id !== holiday.id) }))} /></li>)}</ul> : <p className="py-3 text-center text-xs text-fg-muted">No holidays configured.</p>}</CardBody></Card>
              </Section>
            </div>
          </div>
        )}
      </PageBody>
    </Page>
  );
}
