import {
  Button,
  Card,
  CardBody,
  CardHeader,
  ConfirmDialog,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  Switch,
} from "@hubchat/shared";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

/** Workspace settings (§6.1). */
export default function General() {
  const { workspace } = useWorkspace();
  const [confirmDelete, setConfirmDelete] = useState(false);

  return (
    <Page>
      <PageHeader
        title="General"
        description="Identity, locale, and defaults for this workspace."
        actions={
          <Button variant="primary" size="sm">
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        <Section title="Identity">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Workspace name" htmlFor="ws-name">
                <Input id="ws-name" inputSize="sm" defaultValue={workspace.name} />
              </SettingsRow>

              <SettingsRow
                label="Slug"
                description="Appears in portal URLs and API scoping. Changing it breaks existing links."
                htmlFor="ws-slug"
              >
                <Input id="ws-slug" inputSize="sm" mono defaultValue={workspace.slug} suffix=".hubchat.app" />
              </SettingsRow>

              <SettingsRow
                label="Ticket prefix"
                description="Prepended to display numbers, e.g. SUP-1042. Existing tickets keep their current prefix."
                htmlFor="ws-prefix"
              >
                <Input id="ws-prefix" inputSize="sm" mono defaultValue={workspace.ticket_prefix} className="max-w-32" />
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
                  defaultValue={workspace.timezone}
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
                  defaultValue={workspace.default_language}
                  aria-label="Default language"
                  options={[
                    { value: "en", label: "English" },
                    { value: "pt", label: "Português" },
                    { value: "ja", label: "日本語" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow label="Date format">
                <Select
                  size="sm"
                  defaultValue="dmy"
                  aria-label="Date format"
                  options={[
                    { value: "dmy", label: "12 Mar 2026" },
                    { value: "mdy", label: "Mar 12, 2026" },
                    { value: "iso", label: "2026-03-12" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow label="Time format">
                <Select
                  size="sm"
                  defaultValue="24"
                  aria-label="Time format"
                  options={[
                    { value: "24", label: "24-hour (14:08)" },
                    { value: "12", label: "12-hour (2:08 PM)" },
                  ]}
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Defaults">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Default ticket priority">
                <Select
                  size="sm"
                  defaultValue="normal"
                  aria-label="Default priority"
                  options={[
                    { value: "low", label: "Low" },
                    { value: "normal", label: "Normal" },
                    { value: "high", label: "High" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Auto-assign on first reply"
                description="An unassigned conversation becomes owned by whoever replies first."
              >
                <Switch defaultChecked aria-label="Auto-assign on first reply" />
              </SettingsRow>

              <SettingsRow
                label="Reopen on customer reply"
                description="A reply to a resolved conversation reopens it instead of creating a new one."
              >
                <Switch defaultChecked aria-label="Reopen on reply" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Danger zone">
          <Card className="border-danger-border">
            <CardHeader
              title="Delete this workspace"
              description="Permanently removes every conversation, ticket, customer, article, and file. This cannot be undone and cannot be recovered from a Hubchat backup."
              actions={
                <Button variant="danger" size="sm" leading={<Trash2 />} onClick={() => setConfirmDelete(true)}>
                  Delete workspace
                </Button>
              }
            />
          </Card>
        </Section>
      </PageBody>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${workspace.name}?`}
        description="8 conversations, 8 tickets, 6 customers, 7 articles, and all associated files will be permanently deleted. Members lose access immediately. Export your data first if you might need it."
        confirmLabel="Delete permanently"
        destructive
        onConfirm={() => setConfirmDelete(false)}
      />
    </Page>
  );
}
