import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CopyField,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Section,
  Tooltip,
  useMutation,
  useInfinite,
  formatRelativeShort,
  type Widget,
  type Paginated,
} from "@hubchat/shared";
import { Copy, Globe, MoreHorizontal, Plus, Sparkles, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";

/** Widget list (§6.4) — a workspace may run several for different brands. */
export default function WidgetList() {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

  const list = useInfinite<Widget>(["widgets"], (cursor, signal) => api.get<Paginated<Widget>>(`/widgets?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const widgets = list.items;

  const remove = useMutation<string, unknown>((id) => api.delete(`/widgets/${id}`), {
    invalidates: [["widgets"]],
  });

  return (
    <Page>
      <PageHeader
        title="Widgets"
        description="Embeddable support surfaces. Each one has its own branding, domain allowlist, and destination inbox."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
            New widget
          </Button>
        }
      />

      <PageBody>
        <Section>
          {list.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading widgets…</p> : list.error ? <Callout tone="danger">{list.error instanceof ApiError ? list.error.message : "Could not load widgets."}</Callout> : widgets.length === 0 ? (
            <EmptyState
              icon={Sparkles}
              title="No widgets yet"
              description="A widget is the fastest way to start receiving conversations — one script tag and you are live."
              action={
                <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
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
                          {formatRelativeShort(widget.updated_at, new Date())} ago
                        </p>

                        <div className="mt-2 flex flex-wrap items-center gap-1.5">
                          {widget.domains.length === 0 ? (
                            <span className="text-2xs text-fg-disabled">No domains allowlisted yet</span>
                          ) : (
                            widget.domains.map((domain) => (
                              <span
                                key={domain}
                                className="inline-flex items-center gap-1 rounded-sm bg-fill px-1.5 py-0.5 font-mono text-2xs text-fg-secondary"
                              >
                                <Globe aria-hidden="true" className="size-2.5" />
                                {domain}
                              </span>
                            ))
                          )}
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
                            <MenuItem icon={<Copy />} onSelect={() => navigator.clipboard.writeText(widget.public_key)}>
                              Copy public key
                            </MenuItem>
                            <MenuSeparator />
                            <MenuItem
                              icon={<Trash2 />}
                              destructive
                              onSelect={() => void remove.mutate(widget.id).catch(() => {})}
                            >
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
              {list.hasMore && <Pagination hasPrevious={false} hasNext onPrevious={() => undefined} onNext={() => void list.fetchNext()} summary={`${widgets.length} widgets loaded`} />}
            </div>
          )}
        </Section>
      </PageBody>

      {creating && (
        <CreateWidgetDialog onClose={() => setCreating(false)} onCreated={(id) => navigate(`/channels/widgets/${id}`)} />
      )}
    </Page>
  );
}

function CreateWidgetDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const [name, setName] = useState("");

  const create = useMutation<string, Widget>((widgetName) => api.post("/widgets", { name: widgetName, inbox_id: null }), {
    invalidates: [["widgets"]],
    onSuccess: (widget) => onCreated(widget.id),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="New widget"
        description="You can customise branding, behaviour, and the domain allowlist next."
        footer={
          <Button
            variant="primary"
            size="sm"
            loading={create.isPending}
            disabled={!name.trim()}
            onClick={() => void create.mutate(name.trim()).catch(() => {})}
          >
            Create widget
          </Button>
        }
      >
        {create.error ? (
          <Callout tone="danger" className="mb-3">
            {create.error instanceof ApiError ? create.error.message : "Could not create this widget."}
          </Callout>
        ) : null}
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Marketing site" autoFocus />
        </Field>
      </DialogContent>
    </Dialog>
  );
}
