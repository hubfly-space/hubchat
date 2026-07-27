import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  CodeBlock,
  ColorPicker,
  EmptyState,
  Field,
  Input,
  Page,
  Section,
  SegmentedControl,
  SettingsRow,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  Textarea,
  Toolbar,
  cn,
} from "@hubchat/shared";
import { Book, Globe, GripVertical, Lightbulb, Megaphone, Plus, Ticket, Trash2 } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { portals } from "../../data/fixtures";
import type { Portal } from "@hubchat/shared";

/** Portal builder (§6.5). */
export default function PortalBuilder() {
  const { portalId } = useParams();
  const source = portals.find((item) => item.id === portalId) ?? portals[0];
  const [draft, setDraft] = useState<Portal | undefined>(source);
  const [tab, setTab] = useState("branding");
  const [previewTheme, setPreviewTheme] = useState<"light" | "dark">("light");

  if (!draft) {
    return (
      <Page>
        <EmptyState icon={Globe} size="lg" title="Portal not found" />
      </Page>
    );
  }

  const setTheme = <K extends keyof Portal["theme"]>(key: K, value: Portal["theme"][K]) =>
    setDraft((current) =>
      current ? { ...current, theme: { ...current.theme, [key]: value } } : current,
    );

  return (
    <Page>
      <Toolbar
        className="h-topbar py-0"
        leading={
          <>
            <span className="truncate text-sm font-medium text-fg">{draft.name}</span>
            <Badge tone={draft.enabled ? "success" : "warning"}>
              {draft.enabled ? "Live" : "Disabled"}
            </Badge>
          </>
        }
        trailing={
          <>
            <Button variant="secondary" size="sm">
              Discard
            </Button>
            <Button variant="primary" size="sm">
              Publish
            </Button>
          </>
        }
      />

      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden lg:max-w-xl">
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
            <div className="shrink-0 border-b border-line bg-surface px-4">
              <TabsList
                items={[
                  { value: "branding", label: "Branding" },
                  { value: "content", label: "Content" },
                  { value: "access", label: "Access" },
                  { value: "domain", label: "Domain" },
                ]}
              />
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              <TabsContent value="branding">
                <Section title="Theme">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow label="Accent colour">
                        <ColorPicker
                          value={draft.theme.accent}
                          onChange={(value) => setTheme("accent", value)}
                          label="Portal accent"
                        />
                      </SettingsRow>
                      <SettingsRow label="Colour mode">
                        <SegmentedControl
                          aria-label="Portal colour mode"
                          value={draft.theme.mode}
                          onValueChange={(value) => setTheme("mode", value as Portal["theme"]["mode"])}
                          options={[
                            { value: "light", label: "Light" },
                            { value: "dark", label: "Dark" },
                            { value: "auto", label: "Auto" },
                          ]}
                        />
                      </SettingsRow>
                      <SettingsRow label="Logo" description="SVG or PNG, at least 128px tall.">
                        <Button variant="secondary" size="sm">
                          Upload logo
                        </Button>
                      </SettingsRow>
                      <SettingsRow label="Favicon">
                        <Button variant="secondary" size="sm">
                          Upload favicon
                        </Button>
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section
                  title="Custom CSS variables"
                  description="Escape hatch for brand details the settings above do not cover. Only variables from the documented token list are applied — arbitrary CSS is not injected."
                >
                  <CodeBlock
                    language="css"
                    code={`--hc-radius-lg: 4px;
--hc-font-sans: "Söhne", system-ui, sans-serif;`}
                  />
                </Section>
              </TabsContent>

              <TabsContent value="content">
                <Section title="Homepage">
                  <Card>
                    <CardBody className="space-y-4">
                      <Field label="Headline">
                        <Input
                          value={draft.theme.headline}
                          onChange={(event) => setTheme("headline", event.target.value)}
                        />
                      </Field>
                      <Field label="Subheadline">
                        <Textarea
                          rows={2}
                          value={draft.theme.subheadline}
                          onChange={(event) => setTheme("subheadline", event.target.value)}
                        />
                      </Field>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Sections" description="What the portal exposes to customers.">
                  <Card>
                    <CardBody className="space-y-3">
                      {(
                        [
                          { key: "tickets", label: "Tickets", icon: Ticket, detail: "Submit and track requests." },
                          { key: "knowledge_base", label: "Knowledge base", icon: Book, detail: "Browse and search guides." },
                          { key: "feedback", label: "Feedback boards", icon: Lightbulb, detail: "Vote on and submit ideas." },
                          { key: "changelog", label: "Changelog", icon: Megaphone, detail: "Published product updates." },
                          { key: "announcements", label: "Announcements", icon: Megaphone, detail: "Banner notices at the top of every page." },
                        ] as const
                      ).map((feature) => (
                        <Switch
                          key={feature.key}
                          label={feature.label}
                          description={feature.detail}
                          checked={draft.features[feature.key]}
                          onCheckedChange={(value) =>
                            setDraft((current) =>
                              current
                                ? { ...current, features: { ...current.features, [feature.key]: value } }
                                : current,
                            )
                          }
                        />
                      ))}
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Navigation">
                  <Card>
                    <CardBody className="space-y-2">
                      {draft.navigation.map((item) => (
                        <div key={item.id} className="flex items-center gap-2">
                          <GripVertical aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
                          <Input inputSize="sm" defaultValue={item.label} aria-label="Link label" />
                          <Input inputSize="sm" mono defaultValue={item.href} aria-label="Link target" />
                          <Button
                            variant="ghost"
                            size="sm"
                            iconOnly
                            aria-label={`Remove ${item.label}`}
                            leading={<Trash2 />}
                          />
                        </div>
                      ))}
                      <Button variant="secondary" size="sm" leading={<Plus />}>
                        Add link
                      </Button>
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              <TabsContent value="access">
                <Section title="How customers sign in">
                  <Card>
                    <CardBody className="space-y-3">
                      {(
                        [
                          { value: "password", label: "Password account", detail: "Customers create a password on your portal." },
                          { value: "magic_link", label: "Email magic link", detail: "No password. A one-time link signs them in." },
                          { value: "ticket_link", label: "One-time ticket link", detail: "Access a single ticket without an account." },
                          { value: "sso_token", label: "SSO token from your app", detail: "A signed token from your application logs them in seamlessly." },
                          { value: "anonymous", label: "Anonymous submission", detail: "Submit without signing in. Email verification still applies." },
                        ] as const
                      ).map((method) => (
                        <Checkbox
                          key={method.value}
                          label={method.label}
                          description={method.detail}
                          defaultChecked={draft.auth_methods.includes(method.value)}
                        />
                      ))}
                    </CardBody>
                  </Card>
                </Section>

                <Section
                  title="What customers can do"
                  description="These decide what the portal API will return, not just what the interface renders."
                >
                  <Card>
                    <CardBody className="space-y-3">
                      <Switch
                        label="See every ticket linked to their email"
                        description="Off means they only see tickets created while signed in."
                        defaultChecked={draft.permissions.view_tickets_by_email}
                      />
                      <Switch
                        label="See their company's tickets"
                        description="Anyone at the same verified domain can read the account's tickets. Consider carefully for shared inboxes."
                        defaultChecked={draft.permissions.view_company_tickets}
                      />
                      <Switch label="Reopen resolved tickets" defaultChecked={draft.permissions.reopen_resolved} />
                      <Switch label="Edit ticket fields" defaultChecked={draft.permissions.edit_fields} />
                      <Switch label="Add other participants" defaultChecked={draft.permissions.add_participants} />
                      <Switch label="Download transcripts" defaultChecked={draft.permissions.download_transcript} />
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              <TabsContent value="domain">
                <Section title="Hubchat subdomain">
                  <Card>
                    <CardBody>
                      <Field label="Subdomain" description="Always available as a fallback.">
                        <Input inputSize="md" defaultValue={draft.subdomain} suffix=".hubchat.app" />
                      </Field>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Custom domain">
                  <Card>
                    <CardBody>
                      <Field label="Domain">
                        <Input inputSize="md" mono defaultValue={draft.custom_domain ?? ""} prefix="https://" />
                      </Field>

                      <Callout tone={draft.domain_status === "active" ? "success" : "warning"} className="mt-4">
                        {draft.domain_status === "active"
                          ? "DNS verified and a certificate has been issued. Traffic is being served."
                          : "Add the record below, then verification runs automatically every few minutes."}
                      </Callout>

                      <CodeBlock
                        className="mt-3"
                        language="dns"
                        code={`CNAME  ${draft.custom_domain ?? "help.example.com"}  →  ${draft.subdomain}.hubchat.app`}
                      />
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Danger zone">
                  <Card className="border-danger-border">
                    <CardHeader
                      title="Disable this portal"
                      description="Customers see a maintenance notice. Ticket data is untouched."
                      actions={
                        <Button variant="danger" size="sm">
                          Disable
                        </Button>
                      }
                    />
                  </Card>
                </Section>
              </TabsContent>
            </div>
          </Tabs>
        </div>

        {/* Preview ------------------------------------------------------- */}
        <aside className="hidden min-w-0 flex-1 flex-col border-l border-line bg-sunken lg:flex">
          <div className="flex shrink-0 items-center justify-between border-b border-line px-4 py-2">
            <span className="text-xs text-fg-muted">Live preview</span>
            <SegmentedControl
              aria-label="Preview theme"
              value={previewTheme}
              onValueChange={setPreviewTheme}
              options={[
                { value: "light", label: "Light" },
                { value: "dark", label: "Dark" },
              ]}
            />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto p-6">
            <div
              data-theme={previewTheme}
              data-branded
              style={{ ["--hc-accent-brand" as string]: draft.theme.accent }}
              className="overflow-hidden rounded-xl border border-line bg-canvas"
            >
              <header className="flex items-center justify-between border-b border-line bg-surface px-5 py-3">
                <span className="text-sm font-semibold text-fg">{draft.name}</span>
                <nav className="flex gap-4 text-xs text-fg-muted">
                  {draft.navigation.map((item) => (
                    <span key={item.id}>{item.label}</span>
                  ))}
                </nav>
              </header>

              <div
                className="px-6 py-12 text-center"
                style={{
                  background: `linear-gradient(180deg, color-mix(in oklab, ${draft.theme.accent} 10%, transparent), transparent)`,
                }}
              >
                <h1 className="text-2xl font-semibold tracking-tighter text-fg">
                  {draft.theme.headline}
                </h1>
                <p className="mx-auto mt-2 max-w-md text-sm text-fg-muted">
                  {draft.theme.subheadline}
                </p>
                <div className="mx-auto mt-5 flex max-w-md items-center gap-2 rounded-lg border border-line bg-surface px-3 py-2.5 shadow-1">
                  <span className="text-xs text-fg-disabled">Search for help…</span>
                </div>
              </div>

              <div className="grid gap-3 p-6 sm:grid-cols-3">
                {["Getting started", "Widget & portal", "API & webhooks"].map((collection) => (
                  <div key={collection} className={cn("rounded-lg border border-line bg-surface p-4")}>
                    <Book aria-hidden="true" className="size-4 text-fg-muted" />
                    <p className="mt-2 text-sm font-medium text-fg">{collection}</p>
                    <p className="mt-0.5 text-xs text-fg-muted">12 articles</p>
                  </div>
                ))}
              </div>

              <footer className="border-t border-line px-6 py-4 text-center text-2xs text-fg-muted">
                {draft.theme.footer_links.map((link) => link.label).join(" · ")}
              </footer>
            </div>
          </div>
        </aside>
      </div>
    </Page>
  );
}
