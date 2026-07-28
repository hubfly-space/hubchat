import {
  api,
  ApiError,
  Avatar,
  Button,
  Callout,
  Card,
  CardBody,
  ColorPicker,
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
import { useWorkspace } from "../../app/workspace-context";

type Settings = {
  branding: { accent_color: string; email_footer: string; hide_branding: boolean };
};

/** Workspace branding (§6.1). */
export default function Branding() {
  const { workspace } = useWorkspace();

  const settings = useQuery<Settings>(["workspace-settings"], (signal) =>
    api.get<Settings>("/workspace/settings", { signal }),
  );

  return (
    <Page>
      <PageHeader
        title="Branding"
        description="Logos and colours used across emails, portals, and widgets that have not overridden them."
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-5">
          These are workspace defaults. Individual widgets and portals can override every value here
          once those builders land — useful when one workspace serves several brands.
        </Callout>

        <Section title="Marks">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Workspace icon"
                description="Generated from the workspace name until logo uploads land."
              >
                <Avatar name={workspace.name} seed={workspace.id} shape="square" size="xl" kind="company" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <QueryBoundary query={settings}>{(data) => <BrandingForm initial={data.branding} />}</QueryBoundary>
      </PageBody>
    </Page>
  );
}

function BrandingForm({ initial }: { initial: Settings["branding"] }) {
  const [accent, setAccent] = useState(initial.accent_color || "#3B6EF6");
  const [footer, setFooter] = useState(initial.email_footer);
  const [hideBranding, setHideBranding] = useState(initial.hide_branding);

  const save = useMutation<Settings["branding"], unknown>(
    (body) => api.patch("/workspace/branding", body),
    { invalidates: [["workspace-settings"]] },
  );

  const dirty =
    accent !== (initial.accent_color || "#3B6EF6") ||
    footer !== initial.email_footer ||
    hideBranding !== initial.hide_branding;

  return (
    <>
      <div className="mb-4 flex justify-end">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty}
          loading={save.isPending}
          onClick={() =>
            void save.mutate({ accent_color: accent, email_footer: footer, hide_branding: hideBranding })
          }
        >
          Save changes
        </Button>
      </div>

      {save.error ? (
        <Callout tone="danger" className="mb-4">
          {save.error instanceof ApiError ? save.error.message : "Could not save branding."}
        </Callout>
      ) : null}
      {save.isSuccess ? (
        <Callout tone="success" className="mb-4">
          Saved.
        </Callout>
      ) : null}

      <Section title="Colour">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow
              label="Brand accent"
              description="Used once widgets and portals exist to apply it."
            >
              <ColorPicker value={accent} onChange={setAccent} label="Brand accent" />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>

      <Section title="Outbound email">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow label="Email footer" htmlFor="email-footer">
              <Input
                id="email-footer"
                inputSize="sm"
                value={footer}
                onChange={(event) => setFooter(event.target.value)}
              />
            </SettingsRow>

            <SettingsRow
              label="Hide Hubchat branding"
              description="Removes the 'Powered by Hubchat' line from customer-facing emails."
            >
              <Switch
                checked={hideBranding}
                onCheckedChange={setHideBranding}
                aria-label="Hide Hubchat branding in email"
              />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>
    </>
  );
}
