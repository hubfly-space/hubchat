import {
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
  Section,
  SettingsRow,
  Switch,
} from "@hubchat/shared";
import { Upload } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

/** Workspace branding (§6.1). */
export default function Branding() {
  const { workspace } = useWorkspace();
  const [accent, setAccent] = useState("#3B6EF6");

  return (
    <Page>
      <PageHeader
        title="Branding"
        description="Logos and colours used across emails, portals, and widgets that have not overridden them."
        actions={
          <Button variant="primary" size="sm">
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-5">
          These are workspace defaults. Individual widgets and portals can override every value here
          — useful when one workspace serves several brands.
        </Callout>

        <Section title="Marks">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Workspace icon"
                description="Square. Used in the workspace switcher and email headers. 256×256 minimum."
              >
                <div className="flex items-center gap-3">
                  <Avatar
                    name={workspace.name}
                    seed={workspace.id}
                    shape="square"
                    size="xl"
                    kind="company"
                  />
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

              <SettingsRow
                label="Full logo"
                description="Wordmark used on portal headers and outbound email. SVG preferred."
              >
                <Button variant="secondary" size="sm" leading={<Upload />}>
                  Upload logo
                </Button>
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Colour">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Brand accent"
                description="Contrast is checked against both light and dark backgrounds on save; a colour that fails is rejected rather than shipped."
              >
                <ColorPicker value={accent} onChange={setAccent} label="Brand accent" />
              </SettingsRow>

              <SettingsRow
                label="Apply to new surfaces"
                description="New widgets and portals inherit this colour instead of the Hubchat default."
              >
                <Switch defaultChecked aria-label="Apply to new surfaces" />
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
                  defaultValue="Northwind Cloud · Rua da Prata 80, Lisboa"
                />
              </SettingsRow>

              <SettingsRow
                label="Show Hubchat branding"
                description="Adds a small 'Powered by Hubchat' line to customer-facing emails."
              >
                <Switch aria-label="Show Hubchat branding in email" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
