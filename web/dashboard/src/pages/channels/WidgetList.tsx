import {
  Badge,
  Button,
  Card,
  CardBody,
  CopyField,
  EmptyState,
  Menu,
  MenuContent,
  MenuItem,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tooltip,
  formatRelativeShort,
} from "@hubchat/shared";
import { Copy, Globe, MoreHorizontal, Plus, Sparkles, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, widgets } from "../../data/fixtures";

/** Widget list (§6.4) — a workspace may run several for different brands. */
export default function WidgetList() {
  const { memberById } = useWorkspace();

  return (
    <Page>
      <PageHeader
        title="Widgets"
        description="Embeddable support surfaces. Each one has its own branding, domain allowlist, and destination inbox."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New widget
          </Button>
        }
      />

      <PageBody>
        <Section>
          {widgets.length === 0 ? (
            <EmptyState
              icon={Sparkles}
              title="No widgets yet"
              description="A widget is the fastest way to start receiving conversations — one script tag and you are live."
              action={
                <Button variant="primary" size="sm" leading={<Plus />}>
                  Create a widget
                </Button>
              }
            />
          ) : (
            <div className="space-y-3">
              {widgets.map((widget) => (
                <Card key={widget.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <span
                        aria-hidden="true"
                        className="mt-0.5 size-9 shrink-0 rounded-lg border border-line-strong"
                        style={{ backgroundColor: widget.appearance.accent }}
                      />

                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            to={`/channels/widgets/${widget.id}`}
                            className="truncate text-sm font-medium text-fg hover:underline"
                          >
                            {widget.name}
                          </Link>
                          <Badge tone={widget.environment === "production" ? "accent" : "neutral"}>
                            {widget.environment}
                          </Badge>
                          {!widget.enabled && <Badge tone="warning">Disabled</Badge>}
                          <Tooltip content="Configuration version — every change is versioned and rollback-able">
                            <span>
                              <Badge tone="neutral">v{widget.version}</Badge>
                            </span>
                          </Tooltip>
                        </div>

                        <p className="mt-1 text-xs text-fg-muted">
                          {widget.modes.join(" · ").replace(/_/g, " ")} · updated{" "}
                          {formatRelativeShort(widget.updated_at, NOW)} ago
                        </p>

                        <div className="mt-2 flex flex-wrap items-center gap-1.5">
                          {widget.domains.map((domain) => (
                            <span
                              key={domain}
                              className="inline-flex items-center gap-1 rounded-sm bg-fill px-1.5 py-0.5 font-mono text-2xs text-fg-secondary"
                            >
                              <Globe aria-hidden="true" className="size-2.5" />
                              {domain}
                            </span>
                          ))}
                        </div>
                      </div>

                      <div className="flex shrink-0 items-center gap-1.5">
                        <Button variant="secondary" size="sm" asChild>
                          <Link to={`/channels/widgets/${widget.id}`}>Customise</Link>
                        </Button>
                        <Menu>
                          <MenuTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              iconOnly
                              aria-label={`Actions for ${widget.name}`}
                              leading={<MoreHorizontal />}
                            />
                          </MenuTrigger>
                          <MenuContent align="end">
                            <MenuItem icon={<Copy />}>Duplicate</MenuItem>
                            <MenuItem>View installation health</MenuItem>
                            <MenuItem>Configuration history</MenuItem>
                            <MenuSeparator />
                            <MenuItem icon={<Trash2 />} destructive>
                              Delete widget
                            </MenuItem>
                          </MenuContent>
                        </Menu>
                      </div>
                    </div>

                    <div className="mt-3 max-w-lg">
                      <CopyField label="Public key" value={widget.public_key} />
                    </div>
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
