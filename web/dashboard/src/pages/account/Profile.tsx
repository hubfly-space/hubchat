import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
} from "@hubchat/shared";
import { ShieldCheck, Upload } from "lucide-react";
import { useWorkspace } from "../../app/workspace-context";

/** Personal profile (§11.1). */
export default function Profile() {
  const { viewer } = useWorkspace();

  return (
    <Page>
      <PageHeader
        title="Profile"
        description="How you appear to your team and, where permitted, to customers."
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
              <SettingsRow
                label="Avatar"
                description="Shown on your replies. Customers see this, so pick something you would put on a business card."
              >
                <div className="flex items-center gap-3">
                  <Avatar name={viewer.name} seed={viewer.id} size="xl" />
                  <div className="flex flex-col gap-1.5">
                    <Button variant="secondary" size="sm" leading={<Upload />}>
                      Upload
                    </Button>
                    <Button variant="ghost" size="sm">
                      Remove
                    </Button>
                  </div>
                </div>
              </SettingsRow>

              <SettingsRow label="Full name" htmlFor="name">
                <Input id="name" inputSize="sm" defaultValue={viewer.name} />
              </SettingsRow>

              <SettingsRow
                label="Display name"
                description="Used on customer-facing replies. Leave empty to use your full name."
                htmlFor="display-name"
              >
                <Input id="display-name" inputSize="sm" placeholder={viewer.name.split(" ")[0]} />
              </SettingsRow>

              <SettingsRow
                label="Signature"
                description="Appended to your public replies. Plain text only."
                htmlFor="signature"
              >
                <Input id="signature" inputSize="sm" placeholder="— Ada, Northwind Support" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Email">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Primary email" description="Used for sign-in and notifications.">
                <div className="flex items-center gap-2">
                  <Input inputSize="sm" defaultValue={viewer.email} />
                  <Badge tone="success" leading={<ShieldCheck />}>
                    Verified
                  </Badge>
                </div>
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Password">
          <Card>
            <CardBody className="space-y-4">
              <Callout tone="info">
                Changing your password signs out every other session on your account. This one stays
                signed in.
              </Callout>

              <SettingsRow label="Current password" htmlFor="current">
                <Input id="current" inputSize="sm" type="password" autoComplete="current-password" />
              </SettingsRow>
              <SettingsRow label="New password" htmlFor="new">
                <Input id="new" inputSize="sm" type="password" autoComplete="new-password" />
              </SettingsRow>
              <SettingsRow label="Confirm new password" htmlFor="confirm">
                <Input id="confirm" inputSize="sm" type="password" autoComplete="new-password" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Two-factor authentication">
          <Card>
            <CardHeader
              title="Authenticator app"
              description="A time-based code from any authenticator, plus ten single-use recovery codes."
              actions={<Badge tone="success">Enabled</Badge>}
            />
            <CardBody className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm">
                View recovery codes
              </Button>
              <Button variant="secondary" size="sm">
                Regenerate codes
              </Button>
              <Button variant="danger-ghost" size="sm">
                Disable two-factor
              </Button>
            </CardBody>
          </Card>
        </Section>

        <Section title="Workspace">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Role" description="Only an owner or admin can change this.">
                <Badge tone="accent">{viewer.role}</Badge>
              </SettingsRow>

              <SettingsRow label="Interface language">
                <Select
                  size="sm"
                  defaultValue="en"
                  aria-label="Interface language"
                  options={[
                    { value: "en", label: "English" },
                    { value: "pt", label: "Português" },
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
