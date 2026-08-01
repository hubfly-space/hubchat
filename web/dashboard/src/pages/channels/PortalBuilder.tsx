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
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  SettingsRow,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  Textarea,
  Toolbar,
  ApiError,
  api,
  idempotencyKey,
  useMutation,
  useQuery,
  cn,
} from "@hubchat/shared";
import { Book, Globe, GripVertical, Lightbulb, Megaphone, Plus, Ticket, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import type { Portal, PortalDomain } from "@hubchat/shared";

/** Portal builder (§6.5). */
export default function PortalBuilder() {
  const { portalId } = useParams();
  const query = useQuery<Record<string, unknown>>(["portal", portalId], (signal) => api.get(`/portals/${encodeURIComponent(portalId ?? "")}`, { signal }), { enabled: Boolean(portalId) });
  const [draft, setDraft] = useState<Portal | undefined>();
  const [savedDraft, setSavedDraft] = useState<Portal | undefined>();
  const [tab, setTab] = useState("branding");
  const [previewTheme, setPreviewTheme] = useState<"light" | "dark">("light");
  const [discardOpen, setDiscardOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [domainInput, setDomainInput] = useState("");
  const save = useMutation<Partial<Portal>, Portal>((input) => api.patch(`/portals/${encodeURIComponent(portalId ?? "")}`, input, { idempotencyKey: idempotencyKey() }), { onSuccess: (value) => { const next = normalizePortal(value); setDraft(next); setSavedDraft(next); } });
  const addDomain = useMutation<{ domain: string }, PortalDomain>((input) => api.post(`/portals/${encodeURIComponent(portalId ?? "")}/domains`, input, { idempotencyKey: idempotencyKey() }), { onSuccess: (value) => { setDraft((current) => current ? { ...current, domains: [...(current.domains ?? []), value] } : current); setDomainInput(""); } });
  const verifyDomain = useMutation<{ domainID: string }, PortalDomain>((input) => api.post(`/portals/${encodeURIComponent(portalId ?? "")}/domains/${encodeURIComponent(input.domainID)}/verify`, {}, {}), { onSuccess: (value) => setDraft((current) => current ? { ...current, domains: (current.domains ?? []).map((item) => item.id === value.id ? value : item) } : current) });
  const deleteDomain = useMutation<{ domainID: string }, { deleted: boolean }>((input) => api.delete(`/portals/${encodeURIComponent(portalId ?? "")}/domains/${encodeURIComponent(input.domainID)}`, { idempotencyKey: idempotencyKey() }), { onSuccess: (_value, input) => setDraft((current) => current ? { ...current, domains: (current.domains ?? []).filter((item) => item.id !== input.domainID) } : current) });

  useEffect(() => {
    if (query.data) {
      const next = normalizePortal(query.data);
      setDraft(next);
      setSavedDraft(next);
    }
  }, [query.data]);

  if (query.isLoading) return <Page><PageHeader title="Portal builder" description="Loading portal configuration…" /><PageBody><p className="text-sm text-fg-muted">Loading live configuration…</p></PageBody></Page>;
  if (query.isError) return <Page><EmptyState icon={Globe} size="lg" title="Portal unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={query.refetch}>Try again</Button>} /></Page>;

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
  const dirty = Boolean(savedDraft && JSON.stringify(draft) !== JSON.stringify(savedDraft));
  const discardChanges = () => {
    if (savedDraft) setDraft(savedDraft);
    setDiscardOpen(false);
  };
  const setPermission = (key: keyof Portal["permissions"], value: boolean) => setDraft((current) => current ? { ...current, permissions: { ...current.permissions, [key]: value } } : current);
  const setAuthMethod = (method: Portal["auth_methods"][number], enabled: boolean) => setDraft((current) => current ? { ...current, auth_methods: enabled ? [...new Set([...current.auth_methods, method])] : current.auth_methods.filter((item) => item !== method) } : current);
  const updateNavigation = (id: string, patch: Partial<Portal["navigation"][number]>) => setDraft((current) => current ? { ...current, navigation: current.navigation.map((item) => item.id === id ? { ...item, ...patch } : item) } : current);

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
            <Button variant="secondary" size="sm" disabled={!dirty} onClick={() => setDiscardOpen(true)}>
              Discard changes
            </Button>
            <Button variant="primary" size="sm" loading={save.isPending} onClick={() => void save.mutate({ subdomain: draft.subdomain, theme: draft.theme, features: draft.features, auth_methods: draft.auth_methods, permissions: draft.permissions, navigation: draft.navigation })}>
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
                        <Input inputSize="sm" value={draft.theme.logo_url ?? ""} onChange={(event) => setTheme("logo_url", event.target.value || null)} placeholder="https://cdn.example.com/logo.svg" aria-label="Logo URL" />
                      </SettingsRow>
                      <SettingsRow label="Favicon">
                        <Input inputSize="sm" value={draft.theme.favicon_url ?? ""} onChange={(event) => setTheme("favicon_url", event.target.value || null)} placeholder="https://cdn.example.com/favicon.png" aria-label="Favicon URL" />
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
                          <Input inputSize="sm" value={item.label} onChange={(event) => updateNavigation(item.id, { label: event.target.value })} aria-label="Link label" />
                          <Input inputSize="sm" mono value={item.href} onChange={(event) => updateNavigation(item.id, { href: event.target.value })} aria-label="Link target" />
                          <Button
                            variant="ghost"
                            size="sm"
                            iconOnly
                            aria-label={`Remove ${item.label}`}
                            leading={<Trash2 />}
                            onClick={() => setDraft((current) => current ? { ...current, navigation: current.navigation.filter((candidate) => candidate.id !== item.id) } : current)}
                          />
                        </div>
                      ))}
                      <Button variant="secondary" size="sm" leading={<Plus />} onClick={() => setDraft((current) => current ? { ...current, navigation: [...current.navigation, { id: `nav_${crypto.randomUUID()}`, label: "New link", href: "/", external: false }] } : current)}>
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
                          checked={draft.auth_methods.includes(method.value)}
                          onCheckedChange={(checked) => setAuthMethod(method.value, checked === true)}
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
                        checked={draft.permissions.view_tickets_by_email}
                        onCheckedChange={(checked) => setPermission("view_tickets_by_email", checked === true)}
                      />
                      <Switch
                        label="See their company's tickets"
                        description="Anyone at the same verified domain can read the account's tickets. Consider carefully for shared inboxes."
                        checked={draft.permissions.view_company_tickets}
                        onCheckedChange={(checked) => setPermission("view_company_tickets", checked === true)}
                      />
                      <Switch label="Reopen resolved tickets" checked={draft.permissions.reopen_resolved} onCheckedChange={(checked) => setPermission("reopen_resolved", checked === true)} />
                      <Switch label="Edit ticket fields" checked={draft.permissions.edit_fields} onCheckedChange={(checked) => setPermission("edit_fields", checked === true)} />
                      <Switch label="Add other participants" checked={draft.permissions.add_participants} onCheckedChange={(checked) => setPermission("add_participants", checked === true)} />
                      <Switch label="Download transcripts" checked={draft.permissions.download_transcript} onCheckedChange={(checked) => setPermission("download_transcript", checked === true)} />
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              <TabsContent value="domain">
                <Section title="Hubchat subdomain">
                  <Card>
                    <CardBody>
                      <Field label="Subdomain" description="Always available as a fallback.">
                        <Input inputSize="md" value={draft.subdomain} onChange={(event) => setDraft((current) => current ? { ...current, subdomain: event.target.value } : current)} suffix=".hubchat.app" />
                      </Field>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Custom domain">
                  <Card>
                    <CardBody className="space-y-4">
                      <div className="flex gap-2"><Input inputSize="md" mono value={domainInput} onChange={(event) => setDomainInput(event.target.value)} placeholder="support.example.com" /><Button variant="secondary" size="sm" leading={<Plus />} disabled={!domainInput.trim()} loading={addDomain.isPending} onClick={() => void addDomain.mutate({ domain: domainInput.trim() }).catch(() => {})}>Add</Button></div>
                      <p className="text-xs text-fg-muted">Add a hostname you control, then publish the displayed TXT record before verifying it. Verified domains resolve this portal directly.</p>
                      {(draft.domains ?? []).length > 0 && <div className="space-y-2">{(draft.domains ?? []).map((domain) => <div key={domain.id} className="rounded-md border border-line p-3"><div className="flex items-center gap-2"><span className="min-w-0 flex-1 truncate font-mono text-sm text-fg">https://{domain.domain}</span><Badge tone={domain.status === "verified" ? "success" : domain.status === "failed" ? "danger" : "warning"}>{domain.status}</Badge><Button variant="ghost" size="xs" iconOnly aria-label={`Remove ${domain.domain}`} leading={<Trash2 />} onClick={() => void deleteDomain.mutate({ domainID: domain.id }).catch(() => {})} /></div>{domain.status !== "verified" && <><p className="mt-2 text-xs text-fg-muted">Create a TXT record at <code className="font-mono text-fg-secondary">_hubchat-verification.{domain.domain}</code> with value:</p>{domain.verification_token && <code className="mt-1 block break-all rounded bg-inset px-2 py-1 text-xs text-fg-secondary">{domain.verification_token}</code>}<Button className="mt-2" variant="secondary" size="xs" loading={verifyDomain.isPending} onClick={() => void verifyDomain.mutate({ domainID: domain.id }).catch(() => {})}>Check DNS</Button></>}</div>)}</div>}
                      {Boolean(addDomain.error || verifyDomain.error || deleteDomain.error) && <Callout tone="danger">The domain change could not be completed. Check the hostname and try again.</Callout>}
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Danger zone">
                  <Card className="border-danger-border">
                    <CardHeader
                      title="Disable this portal"
                      description="Customers see a maintenance notice. Ticket data is untouched."
                      actions={
                        <Button variant="danger" size="sm" onClick={() => setDisableOpen(true)}>
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
      <ConfirmDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title="Discard unsaved portal changes?"
        description="The builder will return to the last version saved on the server. This only discards local edits; it does not remove the portal."
        confirmLabel="Discard changes"
        destructive
        onConfirm={discardChanges}
      />
      <ConfirmDialog
        open={disableOpen}
        onOpenChange={setDisableOpen}
        title="Disable this portal?"
        description="Customers will see a maintenance response until the portal is enabled again. Existing tickets and customer data remain unchanged."
        confirmLabel="Disable portal"
        destructive
        loading={save.isPending}
        onConfirm={() => void save.mutate({ enabled: false }).then(() => setDisableOpen(false)).catch(() => {})}
      />
    </Page>
  );
}

function normalizePortal(raw: Record<string, unknown>): Portal {
  const theme = (raw.theme as Record<string, unknown> | undefined) ?? {};
  const features = (raw.features as Record<string, unknown> | undefined) ?? {};
  const permissions = (raw.permissions as Record<string, unknown> | undefined) ?? {};
  return {
    id: String(raw.id ?? ""), workspace_id: String(raw.workspace_id ?? ""), name: String(raw.name ?? "Portal"), subdomain: String(raw.subdomain ?? ""),
    custom_domain: (Array.isArray(raw.domains) ? (raw.domains as Array<Record<string, unknown>>).find((item) => item.status === "verified")?.domain as string | undefined : undefined) ?? null,
    domain_status: (Array.isArray(raw.domains) ? ((raw.domains as Array<Record<string, unknown>>).find((item) => item.status === "verified") ? "active" : (raw.domains as Array<Record<string, unknown>>).some((item) => item.status === "failed") ? "error" : (raw.domains as Array<Record<string, unknown>>).length ? "pending" : "unverified") : "unverified") as Portal["domain_status"],
    domains: Array.isArray(raw.domains) ? raw.domains as PortalDomain[] : [], enabled: raw.enabled !== false, updated_at: String(raw.updated_at ?? new Date().toISOString()),
    theme: { accent: String(theme.accent ?? "#3B6EF6"), mode: (theme.mode as Portal["theme"]["mode"]) ?? "light", logo_url: (theme.logo_url as string | null) ?? null, favicon_url: (theme.favicon_url as string | null) ?? null, headline: String(theme.headline ?? "How can we help?"), subheadline: String(theme.subheadline ?? "Search our guides, track your requests, or start a conversation with the team."), footer_links: Array.isArray(theme.footer_links) ? theme.footer_links as Portal["theme"]["footer_links"] : [], custom_css_vars: (theme.custom_css_vars as Record<string, string>) ?? {} },
    features: { tickets: features.tickets !== false, knowledge_base: features.knowledge_base !== false, feedback: features.feedback !== false, changelog: features.changelog !== false, announcements: features.announcements === true },
    auth_methods: Array.isArray(raw.auth_methods) ? raw.auth_methods as Portal["auth_methods"] : ["magic_link"],
    permissions: { view_tickets_by_email: permissions.view_tickets_by_email === true, view_company_tickets: permissions.view_company_tickets === true, reopen_resolved: permissions.reopen_resolved === true, edit_fields: permissions.edit_fields === true, add_participants: permissions.add_participants === true, download_transcript: permissions.download_transcript === true },
    navigation: Array.isArray(raw.navigation) ? raw.navigation as Portal["navigation"] : [],
  };
}
