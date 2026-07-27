import { Callout, Page, PageBody, PageHeader } from "@hubchat/shared";
import { Link } from "react-router-dom";
import CalendarList from "../sla/CalendarList";

/**
 * Business hours reached from the Settings tree.
 *
 * The same calendars power SLA timers and widget availability, so there is one
 * editor and two entry points rather than two editors that drift apart. This
 * page just re-frames it with the settings-side context.
 */
export default function BusinessHours() {
  return (
    <Page>
      <PageHeader
        title="Business hours"
        description="Shared by SLA timers, widget availability, and time-based automation rules."
      />
      <PageBody className="pb-0">
        <Callout tone="info">
          These are the same calendars shown under{" "}
          <Link to="/sla/calendars" className="text-accent-text hover:underline">
            Automation → Business hours
          </Link>
          . Editing them here changes SLA behaviour immediately.
        </Callout>
      </PageBody>
      <CalendarList />
    </Page>
  );
}
