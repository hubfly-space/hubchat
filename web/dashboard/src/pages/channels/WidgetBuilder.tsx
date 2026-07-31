import {
  api,
  ApiError,
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
  CopyField,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  Page,
  RadioGroup,
  Section,
  SegmentedControl,
  Select,
  SettingsRow,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  Textarea,
  Toolbar,
  Tooltip,
  useMutation,
  useQuery,
  useToast,
  formatRelativeShort,
} from "@hubchat/shared";
import {
  Eye,
  History,
  Monitor,
  Moon,
  RotateCcw,
  Smartphone,
  Sparkles,
  Sun,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { WidgetPreview } from "./WidgetPreview";
import type { Widget } from "@hubchat/shared";

type WidgetVersion = {
  id: string;
  version: number;
  modes: string[];
  appearance: Widget["appearance"];
  content: Widget["content"];
  behavior: Widget["behavior"];
  changed_by: string | null;
  note: string | null;
  created_at: string;
};

/**
 * Widget builder (§6.4).
 *
 * Editor left, live preview right — and the preview never scrolls out of view,
 * because every control here is a visual one and blind configuration is how you
 * end up shipping a magenta launcher to production.
 *
 * Changes are held in local draft state and versioned on save, so §6.4's
 * "configuration history and rollback" has something to roll back to.
 */
export default function WidgetBuilder() {
  const { widgetId } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const { inboxes } = useWorkspace();

  const widgetQuery = useQuery<Widget>(
    widgetId ? ["widget", widgetId] : null,
    (signal) => api.get(`/widgets/${widgetId}`, { signal }),
  );

  const [draft, setDraft] = useState<Widget | undefined>(undefined);
  const [tab, setTab] = useState("appearance");
  const [device, setDevice] = useState<"desktop" | "mobile">("desktop");
  const [previewTheme, setPreviewTheme] = useState<"light" | "dark">("dark");
  const [previewState, setPreviewState] = useState<"launcher" | "home" | "chat">("home");
  const [dirty, setDirty] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  // The server is the source of truth; a fresh load (or a refetch after a
  // successful save) replaces the draft. Local edits between saves live only
  // in this component's state until they are explicitly published.
  useEffect(() => {
    if (widgetQuery.data) {
      setDraft(widgetQuery.data);
      setDirty(false);
    }
  }, [widgetQuery.data]);

  const save = useMutation<Widget, Widget>(
    (payload) =>
      api.put(`/widgets/${widgetId}`, {
        name: payload.name,
        inbox_id: payload.inbox_id,
        modes: payload.modes,
        appearance: payload.appearance,
        content: payload.content,
        behavior: payload.behavior,
        environment: payload.environment,
        rollout_percent: payload.rollout_percent,
        enabled: payload.enabled,
        domains: payload.domains,
      }),
    {
      invalidates: [["widget", widgetId ?? ""], ["widgets"]],
      onSuccess: (updated) => {
        setDirty(false);
        toast.success({
          title: `Published v${updated.version}`,
          description: `Live on ${updated.domains.length} domain${updated.domains.length === 1 ? "" : "s"}.`,
        });
      },
    },
  );

  const remove = useMutation<void, unknown>(() => api.delete(`/widgets/${widgetId}`), {
    onSuccess: () => navigate("/channels/widgets"),
  });

  if (widgetQuery.isLoading) return <Page>{null}</Page>;

  if (widgetQuery.error) {
    return (
      <Page>
        <div className="p-6">
          <Callout tone="danger" title="Could not load this widget">
            {widgetQuery.error instanceof ApiError
              ? widgetQuery.error.message
              : "The widget configuration could not be loaded."}
          </Callout>
          <Button variant="secondary" size="sm" className="mt-3" onClick={widgetQuery.refetch}>
            Retry
          </Button>
        </div>
      </Page>
    );
  }

  if (!draft) {
    return (
      <Page>
        <EmptyState icon={Sparkles} size="lg" title="Widget not found" />
      </Page>
    );
  }

  const patch = (updater: (current: Widget) => Widget) => {
    setDraft((current) => (current ? updater(current) : current));
    setDirty(true);
  };

  const setAppearance = <K extends keyof Widget["appearance"]>(
    key: K,
    value: Widget["appearance"][K],
  ) => patch((current) => ({ ...current, appearance: { ...current.appearance, [key]: value } }));

  const setContent = <K extends keyof Widget["content"]>(key: K, value: Widget["content"][K]) =>
    patch((current) => ({ ...current, content: { ...current.content, [key]: value } }));

  const setBehavior = <K extends keyof Widget["behavior"]>(key: K, value: Widget["behavior"][K]) =>
    patch((current) => ({ ...current, behavior: { ...current.behavior, [key]: value } }));

  return (
    <Page>
      <Toolbar
        className="h-topbar py-0"
        leading={
          <>
            <span className="truncate text-sm font-medium text-fg">{draft.name}</span>
            <Badge tone={draft.environment === "production" ? "accent" : "neutral"}>
              {draft.environment}
            </Badge>
            <Badge tone="neutral">v{draft.version}</Badge>
            {dirty && <Badge tone="warning">Unsaved changes</Badge>}
          </>
        }
        trailing={
          <>
            <Button variant="ghost" size="sm" leading={<History />} onClick={() => setHistoryOpen(true)}>
              History
            </Button>
            <Button
              variant="secondary"
              size="sm"
              disabled={!dirty}
              onClick={() => setDiscardOpen(true)}
            >
              Discard
            </Button>
            <Button
              variant="primary"
              size="sm"
              disabled={!dirty}
              loading={save.isPending}
              onClick={() => void save.mutate(draft).catch(() => {})}
            >
              Publish
            </Button>
          </>
        }
      />

      {save.error ? (
        <Callout tone="danger" className="mx-4 mt-3">
          {save.error instanceof ApiError ? save.error.message : "Could not publish this configuration."}
        </Callout>
      ) : null}

      <div className="flex min-h-0 flex-1">
        {/* Editor -------------------------------------------------------- */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden lg:max-w-xl">
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
            <div className="shrink-0 border-b border-line bg-surface px-4">
              <TabsList
                items={[
                  { value: "appearance", label: "Appearance" },
                  { value: "content", label: "Content" },
                  { value: "behavior", label: "Behaviour" },
                  { value: "install", label: "Install" },
                ]}
              />
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              {/* ------------------------------------------------ appearance */}
              <TabsContent value="appearance">
                <Section title="Brand">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow
                        label="Accent colour"
                        description="Used for the launcher, the send button, and agent message bubbles. Contrast against white is checked on save."
                      >
                        <ColorPicker
                          value={draft.appearance.accent}
                          onChange={(value) => setAppearance("accent", value)}
                          label="Accent colour"
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Theme"
                        description="Automatic follows the visitor's system preference."
                      >
                        <SegmentedControl
                          aria-label="Widget theme"
                          value={draft.appearance.theme}
                          onValueChange={(value) =>
                            setAppearance("theme", value as Widget["appearance"]["theme"])
                          }
                          options={[
                            { value: "light", label: "Light", icon: <Sun /> },
                            { value: "dark", label: "Dark", icon: <Moon /> },
                            { value: "auto", label: "Auto" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow label="Font">
                        <Select
                          size="sm"
                          value={draft.appearance.font}
                          onValueChange={(value) =>
                            setAppearance("font", value as Widget["appearance"]["font"])
                          }
                          aria-label="Font"
                          options={[
                            { value: "system", label: "System UI", description: "Fastest — no webfont request" },
                            { value: "inter", label: "Inter" },
                            { value: "geist", label: "Geist" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Hide Hubchat branding"
                        description="Available on self-hosted deployments."
                      >
                        <Switch
                          checked={draft.appearance.hide_branding}
                          onCheckedChange={(value) => setAppearance("hide_branding", value)}
                          aria-label="Hide branding"
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Launcher">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow label="Shape">
                        <SegmentedControl
                          aria-label="Launcher shape"
                          value={draft.appearance.launcher_shape}
                          onValueChange={(value) =>
                            setAppearance(
                              "launcher_shape",
                              value as Widget["appearance"]["launcher_shape"],
                            )
                          }
                          options={[
                            { value: "circle", label: "Circle" },
                            { value: "rounded", label: "Rounded" },
                            { value: "pill", label: "Pill" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow label="Size">
                        <SegmentedControl
                          aria-label="Launcher size"
                          value={draft.appearance.launcher_size}
                          onValueChange={(value) =>
                            setAppearance("launcher_size", value as Widget["appearance"]["launcher_size"])
                          }
                          options={[
                            { value: "sm", label: "S" },
                            { value: "md", label: "M" },
                            { value: "lg", label: "L" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Label"
                        description="Leave empty for an icon-only launcher."
                      >
                        <Input
                          inputSize="sm"
                          value={draft.appearance.launcher_label ?? ""}
                          onChange={(event) =>
                            setAppearance("launcher_label", event.target.value || null)
                          }
                          placeholder="e.g. Need help?"
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Position and panel">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow label="Corner">
                        <SegmentedControl
                          aria-label="Position"
                          value={draft.appearance.position}
                          onValueChange={(value) =>
                            setAppearance("position", value as Widget["appearance"]["position"])
                          }
                          options={[
                            { value: "bottom-left", label: "Bottom left" },
                            { value: "bottom-right", label: "Bottom right" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow label="Offset" description="Distance from the page edge, in pixels.">
                        <div className="flex gap-2">
                          <Input
                            inputSize="sm"
                            type="number"
                            aria-label="Horizontal offset"
                            prefix="X"
                            value={draft.appearance.offset_x}
                            onChange={(event) =>
                              setAppearance("offset_x", Number(event.target.value))
                            }
                          />
                          <Input
                            inputSize="sm"
                            type="number"
                            aria-label="Vertical offset"
                            prefix="Y"
                            value={draft.appearance.offset_y}
                            onChange={(event) =>
                              setAppearance("offset_y", Number(event.target.value))
                            }
                          />
                        </div>
                      </SettingsRow>

                      <SettingsRow label="Panel size">
                        <div className="flex gap-2">
                          <Input
                            inputSize="sm"
                            type="number"
                            aria-label="Panel width"
                            prefix="W"
                            value={draft.appearance.panel_width}
                            onChange={(event) =>
                              setAppearance("panel_width", Number(event.target.value))
                            }
                          />
                          <Input
                            inputSize="sm"
                            type="number"
                            aria-label="Panel height"
                            prefix="H"
                            value={draft.appearance.panel_height}
                            onChange={(event) =>
                              setAppearance("panel_height", Number(event.target.value))
                            }
                          />
                        </div>
                      </SettingsRow>

                      <SettingsRow label="Corner radius">
                        <Input
                          inputSize="sm"
                          type="number"
                          aria-label="Corner radius"
                          suffix="px"
                          value={draft.appearance.radius}
                          onChange={(event) => setAppearance("radius", Number(event.target.value))}
                        />
                      </SettingsRow>

                      <SettingsRow label="Header style">
                        <SegmentedControl
                          aria-label="Header style"
                          value={draft.appearance.header_style}
                          onValueChange={(value) =>
                            setAppearance("header_style", value as Widget["appearance"]["header_style"])
                          }
                          options={[
                            { value: "solid", label: "Solid" },
                            { value: "gradient", label: "Gradient" },
                            { value: "minimal", label: "Minimal" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow label="Message bubbles">
                        <SegmentedControl
                          aria-label="Bubble style"
                          value={draft.appearance.bubble_style}
                          onValueChange={(value) =>
                            setAppearance("bubble_style", value as Widget["appearance"]["bubble_style"])
                          }
                          options={[
                            { value: "rounded", label: "Rounded" },
                            { value: "square", label: "Square" },
                          ]}
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="z-index"
                        description="Raise this only if the widget is hidden behind your own overlays."
                      >
                        <Input
                          inputSize="sm"
                          type="number"
                          mono
                          aria-label="z-index"
                          value={draft.appearance.z_index}
                          onChange={(event) => setAppearance("z_index", Number(event.target.value))}
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              {/* --------------------------------------------------- content */}
              <TabsContent value="content">
                <Callout tone="info" className="mb-4">
                  Every string here is translatable. Add locales in Settings → General, then switch
                  languages with the selector above this panel once more than one is configured.
                </Callout>

                <Section title="Header">
                  <Card>
                    <CardBody className="space-y-4">
                      <Field label="Title">
                        <Input
                          value={draft.content.title}
                          onChange={(event) => setContent("title", event.target.value)}
                        />
                      </Field>
                      <Field label="Subtitle">
                        <Input
                          value={draft.content.subtitle}
                          onChange={(event) => setContent("subtitle", event.target.value)}
                        />
                      </Field>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Messages">
                  <Card>
                    <CardBody className="space-y-4">
                      <Field label="Welcome message" description="The first thing a visitor reads.">
                        <Textarea
                          rows={2}
                          value={draft.content.welcome_message}
                          onChange={(event) => setContent("welcome_message", event.target.value)}
                        />
                      </Field>
                      <Field label="Input placeholder">
                        <Input
                          value={draft.content.input_placeholder}
                          onChange={(event) => setContent("input_placeholder", event.target.value)}
                        />
                      </Field>
                      <Field label="Expected response time">
                        <Input
                          value={draft.content.response_time_text}
                          onChange={(event) => setContent("response_time_text", event.target.value)}
                        />
                      </Field>
                      <Field label="Online message">
                        <Input
                          value={draft.content.online_message}
                          onChange={(event) => setContent("online_message", event.target.value)}
                        />
                      </Field>
                      <Field
                        label="Offline message"
                        description="Shown outside business hours when the widget is set to stay visible."
                      >
                        <Textarea
                          rows={2}
                          value={draft.content.offline_message}
                          onChange={(event) => setContent("offline_message", event.target.value)}
                        />
                      </Field>
                      <Field
                        label="Consent notice"
                        description="Optional. Displayed before the first message is sent (§12)."
                      >
                        <Textarea
                          rows={2}
                          value={draft.content.consent_text ?? ""}
                          onChange={(event) => setContent("consent_text", event.target.value || null)}
                          placeholder="By starting a chat you agree to our privacy policy."
                        />
                      </Field>
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              {/* -------------------------------------------------- behaviour */}
              <TabsContent value="behavior">
                <Section title="When to show">
                  <Card>
                    <CardBody>
                      <RadioGroup
                        variant="cards"
                        aria-label="Trigger"
                        value={draft.behavior.trigger}
                        onValueChange={(value) =>
                          setBehavior("trigger", value as Widget["behavior"]["trigger"])
                        }
                        options={[
                          { value: "immediate", label: "Immediately", description: "As soon as the page loads." },
                          { value: "delay", label: "After a delay", description: "Give the visitor a moment to read first." },
                          { value: "scroll", label: "After scrolling", description: "Once they reach a share of the page." },
                          { value: "event", label: "On an event", description: "When your application calls Hubchat('show')." },
                          { value: "manual", label: "Manual only", description: "Never auto-shows; you control it entirely." },
                        ]}
                      />

                      {draft.behavior.trigger === "delay" && (
                        <Field label="Delay" className="mt-4 max-w-40">
                          <Input
                            inputSize="sm"
                            type="number"
                            suffix="seconds"
                            value={draft.behavior.delay_seconds}
                            onChange={(event) =>
                              setBehavior("delay_seconds", Number(event.target.value))
                            }
                          />
                        </Field>
                      )}
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Where to show">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow
                        label="Include URLs"
                        description="Glob patterns. Empty means every page on an allowed domain."
                      >
                        <Textarea
                          rows={2}
                          className="font-mono text-xs"
                          value={draft.behavior.include_urls.join("\n")}
                          onChange={(event) =>
                            setBehavior("include_urls", event.target.value.split("\n").filter(Boolean))
                          }
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Exclude URLs"
                        description="Checked after includes. Useful for checkout and sign-up flows."
                      >
                        <Textarea
                          rows={2}
                          className="font-mono text-xs"
                          value={draft.behavior.exclude_urls.join("\n")}
                          onChange={(event) =>
                            setBehavior("exclude_urls", event.target.value.split("\n").filter(Boolean))
                          }
                        />
                      </SettingsRow>

                      <SettingsRow label="Devices">
                        <div className="flex gap-4">
                          {(["desktop", "tablet", "mobile"] as const).map((item) => (
                            <Checkbox
                              key={item}
                              label={item}
                              checked={draft.behavior.devices.includes(item)}
                              onCheckedChange={(checked) =>
                                setBehavior(
                                  "devices",
                                  checked
                                    ? [...draft.behavior.devices, item]
                                    : draft.behavior.devices.filter((device) => device !== item),
                                )
                              }
                            />
                          ))}
                        </div>
                      </SettingsRow>

                      <SettingsRow label="Outside business hours">
                        <Select
                          size="sm"
                          value={draft.behavior.outside_hours}
                          onValueChange={(value) =>
                            setBehavior("outside_hours", value as Widget["behavior"]["outside_hours"])
                          }
                          aria-label="Outside business hours"
                          options={[
                            { value: "hide", label: "Hide the widget" },
                            { value: "show_offline", label: "Show with an offline notice" },
                            { value: "show_form", label: "Show a ticket form instead" },
                          ]}
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Identity and session">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow
                        label="Allow anonymous chat"
                        description="Visitors can start a conversation without identifying themselves."
                      >
                        <Switch
                          checked={draft.behavior.allow_anonymous}
                          onCheckedChange={(value) => setBehavior("allow_anonymous", value)}
                          aria-label="Allow anonymous"
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Require a signed identity token"
                        description="Rejects any boot call without a valid token. Use on authenticated surfaces where agents will discuss account data."
                      >
                        <Switch
                          checked={draft.behavior.require_identity}
                          onCheckedChange={(value) => setBehavior("require_identity", value)}
                          aria-label="Require identity"
                        />
                      </SettingsRow>

                      <SettingsRow
                        label="Persist the conversation"
                        description="Keeps the thread open across page navigation and return visits."
                      >
                        <Switch
                          checked={draft.behavior.persist_conversation}
                          onCheckedChange={(value) => setBehavior("persist_conversation", value)}
                          aria-label="Persist conversation"
                        />
                      </SettingsRow>

                      <SettingsRow label="Notification sound">
                        <Switch
                          checked={draft.behavior.sound}
                          onCheckedChange={(value) => setBehavior("sound", value)}
                          aria-label="Sound"
                        />
                      </SettingsRow>

                      <SettingsRow label="Unread badge on the launcher">
                        <Switch
                          checked={draft.behavior.unread_badge}
                          onCheckedChange={(value) => setBehavior("unread_badge", value)}
                          aria-label="Unread badge"
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Routing">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow label="Destination inbox">
                        <Select
                          size="sm"
                          value={draft.inbox_id}
                          onValueChange={(value) => patch((current) => ({ ...current, inbox_id: value }))}
                          aria-label="Inbox"
                          options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))}
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              {/* --------------------------------------------------- install */}
              <TabsContent value="install">
                <Section title="Script tag" description="The fastest option. Works on any site.">
                  <CodeBlock
                    filename="index.html"
                    code={`<script>
  (function(h,u,b){h.Hubchat=h.Hubchat||function(){(h.Hubchat.q=h.Hubchat.q||[]).push(arguments)};
  var s=u.createElement('script');s.async=1;s.src=b;u.head.appendChild(s)})
  (window,document,'https://support.northwind.cloud/widget/v1.js');

  Hubchat('boot', { key: '${draft.public_key}' });
</script>`}
                  />
                </Section>

                <Section title="npm" description="For applications that already have a build step.">
                  <CodeBlock language="bash" code="npm install @hubchat/widget" />
                  <CodeBlock
                    className="mt-2"
                    language="tsx"
                    code={`import { HubchatWidget } from '@hubchat/widget/react';

export function App() {
  return (
    <>
      <YourApp />
      <HubchatWidget
        publicKey="${draft.public_key}"
        host="https://support.northwind.cloud"
        identity={{ token: identityToken }}
      />
    </>
  );
}`}
                  />
                </Section>

                <Section
                  title="Domain allowlist"
                  description="The widget only boots on these origins. Requests from anywhere else are rejected before any configuration is returned (§11.4)."
                >
                  <Card>
                    <CardBody>
                      <Textarea
                        rows={3}
                        className="font-mono text-xs"
                        value={draft.domains.join("\n")}
                        onChange={(event) =>
                          patch((current) => ({
                            ...current,
                            domains: event.target.value.split("\n").filter(Boolean),
                          }))
                        }
                        aria-label="Allowed domains"
                      />
                      <p className="mt-2 text-xs text-fg-muted">One per line. Wildcards allowed on subdomains: <code className="font-mono">*.northwind.cloud</code></p>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Keys">
                  <div className="space-y-2">
                    <CopyField label="Public key" value={draft.public_key} />
                    <Callout tone="warning">
                      The public key is safe to expose in browser code. It only identifies which
                      widget to load — it grants no read access to conversations.
                    </Callout>
                  </div>
                </Section>

                <Section title="Rollout">
                  <Card>
                    <CardBody className="pt-0">
                      <SettingsRow
                        label="Rollout percentage"
                        description="Serve this configuration version to a share of eligible visitors."
                      >
                        <Input
                          inputSize="sm"
                          type="number"
                          suffix="%"
                          value={draft.rollout_percent}
                          onChange={(event) =>
                            patch((current) => ({
                              ...current,
                              rollout_percent: Number(event.target.value),
                            }))
                          }
                        />
                      </SettingsRow>

                      <SettingsRow label="Enabled">
                        <Switch
                          checked={draft.enabled}
                          onCheckedChange={(value) =>
                            patch((current) => ({ ...current, enabled: value }))
                          }
                          aria-label="Enabled"
                        />
                      </SettingsRow>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Danger zone">
                  <Card className="border-danger-border">
                    <CardHeader
                      title="Delete this widget"
                      description="Existing conversations are kept. The script stops loading on every site within a minute."
                      actions={
                        <Button
                          variant="danger"
                          size="sm"
                          leading={<Trash2 />}
                          loading={remove.isPending}
                          onClick={() => setDeleteOpen(true)}
                        >
                          Delete
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
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-line px-4 py-2">
            <span className="flex items-center gap-1.5 text-xs text-fg-muted">
              <Eye aria-hidden="true" className="size-3.5" />
              Live preview
            </span>

            <div className="flex items-center gap-1.5">
              <SegmentedControl
                aria-label="Preview state"
                value={previewState}
                onValueChange={setPreviewState}
                options={[
                  { value: "launcher", label: "Launcher" },
                  { value: "home", label: "Home" },
                  { value: "chat", label: "Chat" },
                ]}
              />
              <Tooltip content="Preview theme">
                <span>
                  <SegmentedControl
                    aria-label="Preview theme"
                    value={previewTheme}
                    onValueChange={setPreviewTheme}
                    options={[
                      { value: "light", icon: <Sun />, ariaLabel: "Light" },
                      { value: "dark", icon: <Moon />, ariaLabel: "Dark" },
                    ]}
                  />
                </span>
              </Tooltip>
              <SegmentedControl
                aria-label="Preview device"
                value={device}
                onValueChange={setDevice}
                options={[
                  { value: "desktop", icon: <Monitor />, ariaLabel: "Desktop" },
                  { value: "mobile", icon: <Smartphone />, ariaLabel: "Mobile" },
                ]}
              />
            </div>
          </div>

          <div className="flex min-h-0 flex-1 items-center justify-center p-6">
            <div
              className="h-full w-full transition-[max-width] duration-slow ease-out"
              style={{ maxWidth: device === "mobile" ? 390 : undefined }}
            >
              <WidgetPreview
                widget={draft}
                device={device}
                theme={previewTheme}
                state={previewState}
              />
            </div>
          </div>
        </aside>
      </div>

      {historyOpen && widgetId && (
        <WidgetHistoryDialog
          widgetId={widgetId}
          onClose={() => setHistoryOpen(false)}
          onRolledBack={() => setHistoryOpen(false)}
        />
      )}

      <ConfirmDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        title="Discard unpublished widget changes?"
        description="The editor will return to the last configuration saved on the server. Your unpublished changes cannot be recovered."
        confirmLabel="Discard changes"
        onConfirm={() => {
          setDraft(widgetQuery.data);
          setDirty(false);
          setDiscardOpen(false);
        }}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title="Delete this widget?"
        description="The widget script will stop loading on allowed domains. Existing conversations and their history will be kept."
        confirmLabel="Delete widget"
        destructive
        loading={remove.isPending}
        confirmationPhrase={draft.name}
        onConfirm={() => void remove.mutate().then(() => setDeleteOpen(false)).catch(() => {})}
      />
    </Page>
  );
}

function WidgetHistoryDialog({
  widgetId,
  onClose,
  onRolledBack,
}: {
  widgetId: string;
  onClose: () => void;
  onRolledBack: () => void;
}) {
  const versions = useQuery<{ data: WidgetVersion[] }>(["widget-versions", widgetId], (signal) =>
    api.get(`/widgets/${widgetId}/versions`, { signal }),
  );

  const rollback = useMutation<number, Widget>(
    (version) => api.post(`/widgets/${widgetId}/rollback`, { version }),
    { invalidates: [["widget", widgetId], ["widgets"], ["widget-versions", widgetId]], onSuccess: onRolledBack },
  );

  const rows = versions.data?.data ?? [];

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title="Configuration history" description="Every publish is a full snapshot — roll back to any of them." size="md">
        {rollback.error ? (
          <Callout tone="danger" className="mb-3">
            {rollback.error instanceof ApiError ? rollback.error.message : "Could not roll back."}
          </Callout>
        ) : null}
        {rows.length === 0 ? (
          <p className="py-6 text-center text-xs text-fg-muted">No history yet.</p>
        ) : (
          <ul className="divide-y divide-line-subtle">
            {rows.map((version, index) => (
              <li key={version.id} className="flex items-center justify-between gap-3 py-2.5">
                <div className="min-w-0">
                  <p className="text-sm text-fg">
                    v{version.version}
                    {index === 0 && <span className="ml-1.5 text-2xs text-fg-muted">(current)</span>}
                  </p>
                  <p className="mt-0.5 text-xs text-fg-muted">
                    {formatRelativeShort(version.created_at, new Date())} ago
                    {version.note ? ` · ${version.note}` : ""}
                  </p>
                </div>
                {index !== 0 && (
                  <Button
                    variant="secondary"
                    size="xs"
                    leading={<RotateCcw />}
                    loading={rollback.isPending}
                    onClick={() => void rollback.mutate(version.version).catch(() => {})}
                  >
                    Roll back
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
