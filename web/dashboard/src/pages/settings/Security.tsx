import {
  api,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  Input,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  SettingsRow,
  Switch,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { useState } from "react";

type Settings = {
  security: {
    require_two_factor: boolean;
    restrict_signup: boolean;
    allowed_email_domains: string[];
    ip_allowlist: string[];
  };
};

/** Authentication and access policy (§5.1/§5.2). */
export default function Security() {
  const settings = useQuery<Settings>(["workspace-settings"], (signal) =>
    api.get<Settings>("/workspace/settings", { signal }),
  );

  return (
    <Page>
      <PageHeader
        title="Security"
        description="Who can sign up, and what protects sign-in for this workspace."
      />

      <PageBody width="narrow">
        <QueryBoundary query={settings}>{(data) => <SecurityForm initial={data.security} />}</QueryBoundary>
      </PageBody>
    </Page>
  );
}

function SecurityForm({ initial }: { initial: Settings["security"] }) {
  const [requireTwoFactor, setRequireTwoFactor] = useState(initial.require_two_factor);
  const [restrictSignup, setRestrictSignup] = useState(initial.restrict_signup);
  const [domains, setDomains] = useState(initial.allowed_email_domains.join(", "));
  const [ipAllowlist, setIpAllowlist] = useState(initial.ip_allowlist.join(", "));

  const save = useMutation<Settings["security"], unknown>(
    (body) => api.patch("/workspace/security", body),
    { invalidates: [["workspace-settings"]] },
  );

  const parsedDomains = domains
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  const parsedIps = ipAllowlist
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);

  const dirty =
    requireTwoFactor !== initial.require_two_factor ||
    restrictSignup !== initial.restrict_signup ||
    parsedDomains.join(",") !== initial.allowed_email_domains.join(",") ||
    parsedIps.join(",") !== initial.ip_allowlist.join(",");

  return (
    <>
      <div className="mb-4 flex justify-end">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty}
          loading={save.isPending}
          onClick={() =>
            void save.mutate({
              require_two_factor: requireTwoFactor,
              restrict_signup: restrictSignup,
              allowed_email_domains: parsedDomains,
              ip_allowlist: parsedIps,
            })
          }
        >
          Save changes
        </Button>
      </div>

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

      <Section title="Two-factor authentication">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow
              label="Require two-factor for everyone"
              description="Members without an enrolled authenticator are blocked at sign-in once this is on."
            >
              <Switch
                checked={requireTwoFactor}
                onCheckedChange={setRequireTwoFactor}
                aria-label="Require two-factor for everyone"
              />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>

      <Section title="Access control">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow
              label="Restrict sign-up"
              description="When on, only invited addresses can create an account on this workspace."
            >
              <Switch
                checked={restrictSignup}
                onCheckedChange={setRestrictSignup}
                aria-label="Restrict sign-up"
              />
            </SettingsRow>

            <SettingsRow
              label="Allowed email domains"
              description="Comma-separated. Invitations to addresses outside these domains are refused. Empty means no restriction."
            >
              <Input
                inputSize="sm"
                mono
                value={domains}
                onChange={(event) => setDomains(event.target.value)}
                placeholder="example.com"
              />
            </SettingsRow>

            <SettingsRow
              label="IP allowlist"
              description="Comma-separated CIDR ranges. Blocks dashboard access from outside these ranges. Empty means no restriction."
            >
              <Input
                inputSize="sm"
                mono
                value={ipAllowlist}
                onChange={(event) => setIpAllowlist(event.target.value)}
                placeholder="203.0.113.0/24, 2001:db8::/32"
              />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>
    </>
  );
}
