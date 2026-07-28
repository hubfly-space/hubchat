import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Dialog,
  DialogContent,
  Field,
  Input,
  Select,
  Textarea,
  useDebounced,
  useMutation,
  useQuery,
  type Customer,
  type Ticket,
} from "@hubchat/shared";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

type DuplicateCandidate = { id: string; number: number; prefix: string; title: string; status: string };

/**
 * New-ticket dialog (§6.3). Kept deliberately small — title, description,
 * inbox, priority, and an optional customer lookup — rather than surfacing
 * every field a ticket can eventually carry; custom fields are edited from
 * the detail page once the ticket exists, the same way a conversation's
 * attributes are filled in after the fact rather than at creation.
 */
export function NewTicketDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (ticket: Ticket) => void;
}) {
  const { inboxes } = useWorkspace();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [inboxId, setInboxId] = useState("");
  const [priority, setPriority] = useState("normal");
  const [customerQuery, setCustomerQuery] = useState("");
  const [customerId, setCustomerId] = useState<string | null>(null);

  const defaultInboxId = inboxId || inboxes.find((i) => i.is_default)?.id || inboxes[0]?.id || "";

  const customerResults = useQuery<{ data: Customer[] }>(
    customerQuery.trim().length > 1 ? ["customers", "search", customerQuery] : null,
    (signal) => api.get(`/customers?q=${encodeURIComponent(customerQuery)}&limit=5`, { signal }),
  );

  // Deterministic duplicate detection (§6.3): only checked once there is
  // both a title to compare and a customer to scope it to — an unscoped,
  // workspace-wide title match would flag unrelated tickets that merely
  // share a common phrase.
  const debouncedTitle = useDebounced(title, 400);
  const duplicates = useQuery<{ data: DuplicateCandidate[] }>(
    debouncedTitle.trim().length > 3 && customerId ? ["tickets", "duplicates", debouncedTitle, customerId] : null,
    (signal) =>
      api.get(`/tickets/duplicates?title=${encodeURIComponent(debouncedTitle)}&customer_id=${customerId}`, { signal }),
  );

  const create = useMutation<void, Ticket>(
    () =>
      api.post<Ticket>("/tickets", {
        title,
        description,
        inbox_id: defaultInboxId,
        channel: "manual",
        priority,
        customer_id: customerId,
      }),
    {
      invalidates: [["tickets"]],
      onSuccess: (created) => {
        setTitle("");
        setDescription("");
        setCustomerId(null);
        setCustomerQuery("");
        onOpenChange(false);
        onCreated(created);
      },
    },
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="New ticket"
        footer={
          <Button
            variant="primary"
            size="sm"
            loading={create.isPending}
            disabled={!title.trim() || !defaultInboxId}
            onClick={() => void create.mutate().catch(() => {})}
          >
            Create ticket
          </Button>
        }
      >
        {create.error ? (
          <Callout tone="danger" className="mb-3">
            {create.error instanceof ApiError ? create.error.message : "Could not create the ticket."}
          </Callout>
        ) : null}

        <div className="flex flex-col gap-3">
          <Field label="Title">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What's going on?" autoFocus />
          </Field>
          {duplicates.data && duplicates.data.data.length > 0 && (
            <Callout tone="warning">
              <p className="mb-1.5 font-medium">This customer may already have a similar ticket open:</p>
              <ul className="flex flex-col gap-1">
                {duplicates.data.data.map((d) => (
                  <li key={d.id}>
                    <Link to={`/tickets/${d.id}`} target="_blank" className="flex items-center gap-1.5 text-xs hover:underline">
                      <Badge tone="neutral">
                        {d.prefix}-{d.number}
                      </Badge>
                      <span className="truncate">{d.title}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </Callout>
          )}
          <Field label="Description">
            <Textarea
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Details the requester or agent provided."
            />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Inbox">
              <Select
                value={defaultInboxId}
                onValueChange={setInboxId}
                options={inboxes.map((i) => ({ value: i.id, label: i.name }))}
                aria-label="Inbox"
              />
            </Field>
            <Field label="Priority">
              <Select
                value={priority}
                onValueChange={setPriority}
                options={[
                  { value: "urgent", label: "Urgent" },
                  { value: "high", label: "High" },
                  { value: "normal", label: "Normal" },
                  { value: "low", label: "Low" },
                ]}
                aria-label="Priority"
              />
            </Field>
          </div>
          <Field label="Customer (optional)">
            <Input
              value={customerId ? (customerResults.data?.data.find((c) => c.id === customerId)?.name ?? customerQuery) : customerQuery}
              onChange={(e) => {
                setCustomerId(null);
                setCustomerQuery(e.target.value);
              }}
              placeholder="Search by name or email…"
            />
            {!customerId && customerResults.data && customerResults.data.data.length > 0 && (
              <ul className="mt-1 flex flex-col gap-0.5 rounded-md border border-line bg-surface p-1">
                {customerResults.data.data.map((c) => (
                  <li key={c.id}>
                    <button
                      type="button"
                      className="w-full rounded-sm px-2 py-1 text-left text-xs text-fg-secondary hover:bg-fill"
                      onClick={() => {
                        setCustomerId(c.id);
                        setCustomerQuery(c.name ?? "");
                      }}
                    >
                      {c.name ?? "Unnamed"} {c.email ? `· ${c.email}` : ""}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}
