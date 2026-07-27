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
  Select,
  cn,
} from "@hubchat/shared";
import { CalendarClock, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { calendars } from "../../data/fixtures";

const DAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

/** Business hours and holidays (§6.14). */
export default function CalendarList() {
  const [activeId, setActiveId] = useState(calendars[0]!.id);
  const active = calendars.find((calendar) => calendar.id === activeId)!;

  return (
    <Page>
      <PageHeader
        title="Business hours"
        description="Calendars used by SLA timers, widget availability, and time-based rules."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New calendar
          </Button>
        }
      />

      <PageBody>
        <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
          <nav aria-label="Calendars" className="space-y-1">
            {calendars.map((calendar) => (
              <button
                key={calendar.id}
                type="button"
                onClick={() => setActiveId(calendar.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors",
                  calendar.id === activeId
                    ? "bg-accent-subtle font-medium text-fg"
                    : "text-fg-secondary hover:bg-fill hover:text-fg",
                )}
              >
                <CalendarClock aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                <span className="min-w-0 flex-1 truncate">{calendar.name}</span>
                {calendar.is_default && <Badge tone="neutral">Default</Badge>}
              </button>
            ))}
          </nav>

          <div className="min-w-0">
            <Callout tone="info" className="mb-4">
              SLA timers advance only inside these windows. A ticket opened at 17:55 on Friday with
              a two-hour target is not late until Monday morning.
            </Callout>

            <Section title="Weekly schedule">
              <Card>
                <CardHeader
                  title={active.name}
                  description={`All times in ${active.timezone}.`}
                  actions={
                    <Select
                      size="sm"
                      value={active.timezone}
                      aria-label="Timezone"
                      className="w-52"
                      options={[
                        { value: "Europe/Lisbon", label: "Europe/Lisbon" },
                        { value: "UTC", label: "UTC" },
                        { value: "America/New_York", label: "America/New_York" },
                        { value: "Asia/Singapore", label: "Asia/Singapore" },
                      ]}
                    />
                  }
                />
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {DAYS.map((day, index) => {
                      const windows = active.weekly[index] ?? [];
                      const closed = windows.length === 0;

                      return (
                        <li key={day} className="flex items-center gap-4 px-4 py-2.5">
                          <span
                            className={cn(
                              "w-24 shrink-0 text-sm",
                              closed ? "text-fg-disabled" : "text-fg",
                            )}
                          >
                            {day}
                          </span>

                          {closed ? (
                            <span className="text-xs text-fg-muted">Closed</span>
                          ) : (
                            <div className="flex flex-wrap items-center gap-2">
                              {windows.map((window, windowIndex) => (
                                <span
                                  key={windowIndex}
                                  className="inline-flex items-center gap-1.5 rounded-md border border-line bg-inset px-2 py-1 font-mono text-xs tabular text-fg-secondary"
                                >
                                  {window.start} – {window.end}
                                </span>
                              ))}
                            </div>
                          )}

                          <Button variant="ghost" size="xs" className="ml-auto shrink-0">
                            {closed ? "Add hours" : "Edit"}
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section
              title="Holidays"
              description="Full days the team is closed. Timers pause for the whole day in this calendar's timezone."
              actions={
                <Button variant="secondary" size="sm" leading={<Plus />}>
                  Add holiday
                </Button>
              }
            >
              <Card>
                <CardBody className="p-0">
                  {active.holidays.length === 0 ? (
                    <p className="px-4 py-6 text-center text-xs text-fg-muted">
                      No holidays configured.
                    </p>
                  ) : (
                    <ul className="divide-y divide-line-subtle">
                      {active.holidays.map((holiday) => (
                        <li key={holiday.date} className="flex items-center gap-3 px-4 py-2.5">
                          <span className="w-28 shrink-0 font-mono text-xs tabular text-fg-muted">
                            {holiday.date}
                          </span>
                          <span className="min-w-0 flex-1 truncate text-sm text-fg">
                            {holiday.label}
                          </span>
                          <Button
                            variant="ghost"
                            size="xs"
                            iconOnly
                            aria-label={`Remove ${holiday.label}`}
                            leading={<Trash2 />}
                          />
                        </li>
                      ))}
                    </ul>
                  )}
                </CardBody>
              </Card>
            </Section>
          </div>
        </div>
      </PageBody>
    </Page>
  );
}
