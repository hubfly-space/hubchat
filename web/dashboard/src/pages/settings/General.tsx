import {
  api,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  Input,
  invalidate,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  useMutation,
} from "@hubchat/shared";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

type GeneralPayload = {
  name: string;
  ticket_prefix: string;
  timezone: string;
  default_language: string;
};

/** Workspace settings (§6.1). */
export default function General() {
  const { workspace } = useWorkspace();

  const [name, setName] = useState(workspace.name);
  const [ticketPrefix, setTicketPrefix] = useState(workspace.ticket_prefix);
  const [timezone, setTimezone] = useState(workspace.timezone);
  const [language, setLanguage] = useState(workspace.default_language);

  const dirty =
    name.trim() !== workspace.name ||
    ticketPrefix.trim().toUpperCase() !== workspace.ticket_prefix ||
    timezone !== workspace.timezone ||
    language !== workspace.default_language;

  const save = useMutation<GeneralPayload, unknown>((body) => api.patch("/workspace/general", body), {
    invalidates: [["bootstrap"]],
    onSuccess: () => invalidate(["bootstrap"]),
  });

  return (
    <Page>
      <PageHeader
        title="General"
        description="Identity, locale, and defaults for this workspace."
        actions={
          <Button
            variant="primary"
            size="sm"
            disabled={!dirty}
            loading={save.isPending}
            onClick={() =>
              void save.mutate({
                name: name.trim(),
                ticket_prefix: ticketPrefix.trim().toUpperCase(),
                timezone,
                default_language: language,
              })
            }
          >
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        {save.error ? (
          <Callout tone="danger" className="mb-4">
            {save.error instanceof ApiError ? save.error.message : "Could not save these settings."}
          </Callout>
        ) : null}
        {save.isSuccess ? (
          <Callout tone="success" className="mb-4">
            Saved.
          </Callout>
        ) : null}

        <Section title="Identity">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Workspace name" htmlFor="ws-name">
                <Input id="ws-name" inputSize="sm" value={name} onChange={(event) => setName(event.target.value)} />
              </SettingsRow>

              <SettingsRow
                label="Slug"
                description="Appears in portal URLs and API scoping. Not changeable from here yet."
                htmlFor="ws-slug"
              >
                <Input id="ws-slug" inputSize="sm" mono defaultValue={workspace.slug} disabled />
              </SettingsRow>

              <SettingsRow
                label="Ticket prefix"
                description="Prepended to display numbers, e.g. SUP-1042. Existing tickets keep their current prefix."
                htmlFor="ws-prefix"
              >
                <Input
                  id="ws-prefix"
                  inputSize="sm"
                  mono
                  className="max-w-32"
                  value={ticketPrefix}
                  onChange={(event) => setTicketPrefix(event.target.value)}
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Locale">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Timezone"
                description="Every timestamp, report boundary, and business-hours calculation uses this — not the viewer's browser."
              >
                <Select
                  size="sm"
                  value={timezone}
                  onValueChange={setTimezone}
                  aria-label="Timezone"
                  options={[
                    { value: "Europe/Lisbon", label: "Europe/Lisbon" },
                    { value: "Europe/Berlin", label: "Europe/Berlin" },
                    { value: "America/New_York", label: "America/New_York" },
                    { value: "Asia/Singapore", label: "Asia/Singapore" },
                    { value: "UTC", label: "UTC" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow label="Default language">
                <Select
                  size="sm"
                  value={language}
                  onValueChange={setLanguage}
                  aria-label="Default language"
                  options={[
                    { value: "en", label: "English" },
                    { value: "pt", label: "Português" },
                    { value: "ja", label: "日本語" },
                  ]}
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
