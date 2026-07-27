import {
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
  Switch,
} from "@hubchat/shared";
import { ShieldCheck } from "lucide-react";
import { members } from "../../data/fixtures";

/** Authentication and session policy (§11). */
export default function Security() {
  const without2fa = members.filter((member) => member.role !== "developer").length - 2;

  return (
    <Page>
      <PageHeader
        title="Security"
        description="How people sign in to this workspace and how long their sessions last."
        actions={
          <Button variant="primary" size="sm">
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        <Section title="Sign-in methods">
          <Card>
            <CardBody className="space-y-3">
              <Switch
                label="Email and password"
                description="Minimum 12 characters, checked against a breached-password list on set."
                defaultChecked
              />
              <Switch
                label="Email magic link"
                description="Single-use, expires in 15 minutes. Convenient, and one fewer password to leak."
                defaultChecked
              />
              <Switch
                label="OAuth providers"
                description="Configured per deployment. Currently: GitHub."
                defaultChecked
              />
              <Switch
                label="SAML single sign-on"
                description="Available in a later release."
                disabled
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Two-factor authentication">
          <Callout tone="warning" className="mb-3" icon={<ShieldCheck />}>
            {without2fa} member{without2fa === 1 ? " has" : "s have"} not enrolled. Requiring 2FA
            gives them a grace period to enrol before they are locked out.
          </Callout>

          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Require two-factor"
                description="Applies to every member. Owners and admins cannot opt out."
              >
                <Select
                  size="sm"
                  defaultValue="admins"
                  aria-label="2FA requirement"
                  options={[
                    { value: "off", label: "Optional" },
                    { value: "admins", label: "Required for admins and owners" },
                    { value: "all", label: "Required for everyone" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow label="Enrolment grace period" description="Time to enrol before sign-in is blocked.">
                <Input inputSize="sm" type="number" suffix="days" defaultValue={7} className="max-w-32" />
              </SettingsRow>

              <SettingsRow
                label="Recovery codes"
                description="Ten single-use codes generated at enrolment. Regenerating invalidates the previous set."
              >
                <Badge tone="neutral">Enabled</Badge>
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Sessions">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Session lifetime"
                description="How long a session stays valid without activity."
              >
                <Select
                  size="sm"
                  defaultValue="30d"
                  aria-label="Session lifetime"
                  options={[
                    { value: "8h", label: "8 hours" },
                    { value: "7d", label: "7 days" },
                    { value: "30d", label: "30 days" },
                    { value: "90d", label: "90 days" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Rotate on privilege change"
                description="Issues a fresh session identifier whenever a member's role changes, so a stale token cannot retain old permissions."
              >
                <Switch defaultChecked aria-label="Rotate on privilege change" />
              </SettingsRow>

              <SettingsRow
                label="Sign out everywhere on password change"
                description="Recommended. The person changing the password keeps their current session."
              >
                <Switch defaultChecked aria-label="Sign out on password change" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Access control">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Restrict sign-up"
                description="When on, only invited addresses can create an account on this deployment."
              >
                <Switch defaultChecked aria-label="Restrict sign-up" />
              </SettingsRow>

              <SettingsRow
                label="Allowed email domains"
                description="Invitations to addresses outside these domains are refused. Empty means no restriction."
              >
                <Input inputSize="sm" mono defaultValue="northwind.cloud" />
              </SettingsRow>

              <SettingsRow
                label="IP allowlist"
                description="Blocks dashboard access from outside these ranges. Widget and portal traffic is unaffected."
              >
                <Input inputSize="sm" mono placeholder="203.0.113.0/24, 2001:db8::/32" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Rate limiting">
          <Card>
            <CardHeader
              title="Brute-force protection"
              description="Applied per account and per source address. Failures are recorded in the audit log."
            />
            <CardBody className="pt-0">
              <SettingsRow label="Failed sign-in attempts before lockout">
                <Input inputSize="sm" type="number" defaultValue={5} className="max-w-32" />
              </SettingsRow>
              <SettingsRow label="Lockout duration">
                <Input inputSize="sm" type="number" suffix="minutes" defaultValue={15} className="max-w-32" />
              </SettingsRow>
              <SettingsRow label="API requests per minute per key">
                <Input inputSize="sm" type="number" defaultValue={600} className="max-w-32" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
