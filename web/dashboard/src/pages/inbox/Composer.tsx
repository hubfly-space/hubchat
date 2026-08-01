import {
  Button,
  Kbd,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
  Tabs,
  TabsList,
  Textarea,
  Tooltip,
  api,
  cn,
  idempotencyKey,
  invalidate,
  useInfinite,
  useToast,
  type Paginated,
} from "@hubchat/shared";
import {
  Lock,
  MessageSquare,
  MessageSquareReply,
  Paperclip,
  Send,
  Zap,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

type ComposerMode = "reply" | "note";

export type ComposerProps = {
  /** Omitted by the isolated fixture demo, which must not call production APIs. */
  workspaceId?: string;
  /** Macro execution is an explicit automation capability, not a fixture affordance. */
  canUseMacros?: boolean;
  customerName: string;
  ticketNumber?: string;
  /**
   * Drafts autosave to localStorage under this key, per conversation, so a
   * reload or a tab switch never loses what someone was typing. Attachments
   * and saved replies are real API-backed actions.
   */
  conversationId: string;
  onSend: (body: string, kind: ComposerMode, fileIDs: string[]) => Promise<void>;
};

type PendingAttachment = { id: string; name: string };

function draftKey(conversationId: string) {
  return `hubchat.draft.${conversationId}`;
}

/**
 * Agent composer (§6.2).
 *
 * The mode switch is the most consequential control on the screen, so it is not
 * a subtle toggle: switching to a note re-tints the entire composer amber and
 * relabels the send button. There is no state in which "am I about to reply
 * publicly?" requires a second look.
 */
export function Composer({ workspaceId, canUseMacros = false, customerName, ticketNumber, conversationId, onSend }: ComposerProps) {
  const [mode, setMode] = useState<ComposerMode>("reply");
  const [value, setValue] = useState(() => {
    try {
      return localStorage.getItem(draftKey(conversationId)) ?? "";
    } catch {
      return "";
    }
  });
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [attachments, setAttachments] = useState<PendingAttachment[]>([]);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const toast = useToast();
  const savedReplies = useInfinite<SavedReply>(
    workspaceId ? ["composer-saved-replies", workspaceId] : null,
    (cursor, signal) => api.get<Paginated<SavedReply>>(`/automation/replies?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal, workspaceId }),
  );
  const macros = useInfinite<Macro>(
    canUseMacros && workspaceId ? ["composer-macros", workspaceId] : null,
    (cursor, signal) => api.get<Paginated<Macro>>(`/automation/macros?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal, workspaceId }),
  );

  // Switching conversations loads that thread's own draft rather than
  // carrying over whatever was being typed for the last one.
  useEffect(() => {
    try {
      setValue(localStorage.getItem(draftKey(conversationId)) ?? "");
    } catch {
      setValue("");
    }
  }, [conversationId]);

  useEffect(() => {
    try {
      if (value) localStorage.setItem(draftKey(conversationId), value);
      else localStorage.removeItem(draftKey(conversationId));
    } catch {
      // A full or disabled localStorage should not break typing.
    }
  }, [conversationId, value]);

  const isNote = mode === "note";
  const canSend = value.trim().length > 0 && !sending && !uploading;

  const chooseFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const selected of Array.from(files)) {
        const form = new FormData();
        form.append("file", selected);
        form.append("owner_type", "conversation");
        form.append("owner_id", conversationId);
        const uploaded = await api.post<{ id: string; name: string }>("/files", form, { idempotencyKey: idempotencyKey() });
        setAttachments((current) => [...current, { id: uploaded.id, name: uploaded.name }]);
      }
    } catch (error) {
      toast.error({ title: "Could not attach file", description: error instanceof Error ? error.message : "Try again." });
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const send = () => {
    if (!canSend) return;

    const body = value;
    setSending(true);
    onSend(body, mode, attachments.map((attachment) => attachment.id))
      .then(() => {
        setValue("");
        setAttachments([]);
        toast.success({
          title: isNote ? "Note added" : "Reply sent",
          description: isNote ? "Only your team can see this." : `Delivered to ${customerName}.`,
        });
      })
      .catch((error: unknown) => {
        // The draft is kept on failure — losing what someone just typed
        // because the network hiccupped is the one thing a composer must
        // never do.
        toast.error({
          title: "Could not send",
          description: error instanceof Error ? error.message : "Try again.",
        });
      })
      .finally(() => setSending(false));
  };

  const insertSavedReply = async (reply: SavedReply, shortcutToReplace?: string) => {
    const expanded = expandReply(reply.body, { customerName, ticketNumber });
    setValue((current) => {
      let prefix = current.trimEnd();
      if (shortcutToReplace && prefix.endsWith(shortcutToReplace)) {
        prefix = prefix.slice(0, -shortcutToReplace.length).trimEnd();
      }
      return prefix ? `${prefix}\n\n${expanded}` : expanded;
    });
    textareaRef.current?.focus();
    if (!workspaceId) return;
    try {
      await api.post(`/automation/replies/${encodeURIComponent(reply.id)}/use`, {}, { workspaceId, idempotencyKey: idempotencyKey() });
    } catch (error) {
      // Insertion is still useful if the usage counter is temporarily down;
      // make the observability failure visible without losing the agent's
      // prepared reply.
      toast.error({ title: "Reply inserted", description: error instanceof Error ? `Usage could not be recorded: ${error.message}` : "Usage could not be recorded." });
    }
  };

  const executeMacro = async (macro: Macro) => {
    if (!workspaceId) return;
    try {
      await api.post(`/automation/macros/${encodeURIComponent(macro.id)}/use`, {
        subject_type: "conversation",
        subject_id: conversationId,
      }, { workspaceId, idempotencyKey: idempotencyKey() });
      invalidate(["conversations"]);
      invalidate(["conversation", conversationId]);
      invalidate(["conversation-messages", conversationId]);
      toast.success({ title: "Macro applied", description: `${macro.name} ran on this conversation.` });
    } catch (error) {
      toast.error({ title: "Could not apply macro", description: error instanceof Error ? error.message : "Check the macro permissions and try again." });
    }
  };

  return (
    <div
      className={cn(
        "shrink-0 border-t px-3 pb-3 pt-2 transition-colors duration-base",
        isNote ? "border-warning-border bg-warning-subtle" : "border-line bg-surface",
      )}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <Tabs value={mode} onValueChange={(next) => setMode(next as ComposerMode)}>
          <TabsList
            variant="pills"
            items={[
              { value: "reply", label: "Reply", icon: <MessageSquare /> },
              { value: "note", label: "Internal note", icon: <Lock /> },
            ]}
          />
        </Tabs>

        {isNote && (
          <p className="flex items-center gap-1.5 text-2xs font-medium text-warning-text">
            <Lock aria-hidden="true" className="size-3" />
            The customer will not see this
          </p>
        )}
      </div>

      <div
        className={cn(
          "rounded-lg border bg-surface transition-colors",
          isNote ? "border-warning-border" : "border-line",
          "focus-within:border-accent focus-within:shadow-[0_0_0_3px_var(--hc-accent-subtle)]",
          isNote && "focus-within:border-warning focus-within:shadow-[0_0_0_3px_var(--hc-warning-subtle)]",
        )}
      >
        <Textarea
          ref={textareaRef}
          data-editor
          autoResize
          rows={3}
          value={value}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Tab" && !isNote) {
              const shortcut = value.trimEnd().split(/\s+/).at(-1);
              const reply = shortcut ? savedReplies.items.find((item) => item.shortcut === shortcut) : undefined;
              if (reply) {
                event.preventDefault();
                void insertSavedReply(reply, shortcut);
                return;
              }
            }
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              send();
            }
          }}
          placeholder={
            isNote
              ? "Add context for your team. Use @ to mention someone."
              : `Reply to ${customerName}…`
          }
          className="border-0 bg-transparent px-3 py-2.5 shadow-none focus:bg-transparent focus:shadow-none"
          aria-label={isNote ? "Internal note" : "Public reply"}
        />

        {attachments.length > 0 && (
          <div className="flex flex-wrap gap-1.5 border-t border-line-subtle px-3 py-2">
            {attachments.map((attachment) => (
              <span key={attachment.id} className="inline-flex max-w-full items-center gap-1 rounded-md bg-fill px-2 py-1 text-2xs text-fg-secondary">
                <Paperclip aria-hidden="true" className="size-3 shrink-0" />
                <span className="max-w-48 truncate">{attachment.name}</span>
                <button type="button" className="text-fg-muted hover:text-fg" aria-label={`Remove ${attachment.name}`} onClick={() => setAttachments((current) => current.filter((item) => item.id !== attachment.id))}>×</button>
              </span>
            ))}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 border-t border-line-subtle px-2 py-1.5">
          <div className="flex items-center gap-0.5">
            <Tooltip content="Attach file">
              <Button variant="ghost" size="xs" iconOnly aria-label="Attach file" loading={uploading} leading={<Paperclip />} onClick={() => fileInputRef.current?.click()} />
            </Tooltip>
            <input ref={fileInputRef} type="file" multiple className="sr-only" onChange={(event) => void chooseFiles(event.target.files)} />

            {workspaceId && (
              <Menu>
                <MenuTrigger asChild>
                  <Button variant="ghost" size="xs" leading={<MessageSquareReply />} disabled={isNote || savedReplies.isLoading} loading={savedReplies.isFetching && savedReplies.items.length === 0}>
                    Saved reply
                  </Button>
                </MenuTrigger>
                <MenuContent className="w-72">
                  <MenuLabel>Insert saved reply</MenuLabel>
                  {savedReplies.error ? (
                    <MenuItem disabled description="Check your support permissions or try again later.">Saved replies unavailable</MenuItem>
                  ) : savedReplies.items.length === 0 && !savedReplies.isLoading ? (
                    <MenuItem disabled description="Create shared replies under Automation.">No saved replies yet</MenuItem>
                  ) : (
                    savedReplies.items.map((reply) => (
                      <MenuItem key={reply.id} icon={<MessageSquareReply />} onSelect={() => void insertSavedReply(reply)} description={`${reply.shortcut ? `${reply.shortcut} · ` : ""}${expandReply(reply.body, { customerName, ticketNumber }).slice(0, 112)}`}>
                        {reply.name}
                      </MenuItem>
                    ))
                  )}
                  {savedReplies.hasMore && (
                    <>
                      <MenuSeparator />
                      <MenuItem icon={<Zap />} onSelect={() => void savedReplies.fetchNext()} description={savedReplies.isFetching ? "Loading…" : undefined}>Load more replies</MenuItem>
                    </>
                  )}
                </MenuContent>
              </Menu>
            )}

            {workspaceId && canUseMacros && (
              <Menu>
                <MenuTrigger asChild>
                  <Button variant="ghost" size="xs" leading={<Zap />} disabled={isNote || macros.isLoading} loading={macros.isFetching && macros.items.length === 0}>
                    Macro
                  </Button>
                </MenuTrigger>
                <MenuContent className="w-72">
                  <MenuLabel>Apply macro</MenuLabel>
                  {macros.error ? (
                    <MenuItem disabled description="Check your automation permissions or try again later.">Macros unavailable</MenuItem>
                  ) : macros.items.length === 0 && !macros.isLoading ? (
                    <MenuItem disabled description="Create a macro under Automation.">No macros yet</MenuItem>
                  ) : (
                    macros.items.map((macro) => (
                      <MenuItem key={macro.id} icon={<Zap />} onSelect={() => void executeMacro(macro)} description={`${macro.actions.length} action${macro.actions.length === 1 ? "" : "s"}${macro.body ? " · sends a reply" : ""}`}>
                        {macro.name}
                      </MenuItem>
                    ))
                  )}
                  {macros.hasMore && (
                    <>
                      <MenuSeparator />
                      <MenuItem icon={<Zap />} onSelect={() => void macros.fetchNext()} description={macros.isFetching ? "Loading…" : undefined}>Load more macros</MenuItem>
                    </>
                  )}
                </MenuContent>
              </Menu>
            )}
          </div>

          <div className="flex items-center gap-1.5">
            <Button
              size="sm"
              variant={isNote ? "secondary" : "primary"}
              disabled={!canSend}
              loading={sending}
              onClick={send}
              trailing={<Send />}
            >
              {isNote ? "Add note" : "Send"}
            </Button>
          </div>
        </div>
      </div>

      <p className="mt-1.5 flex items-center gap-1.5 text-2xs text-fg-muted">
        <Kbd keys="mod+enter" /> to send
        <span className="ml-auto">Draft saves automatically</span>
      </p>
    </div>
  );
}

type SavedReply = {
  id: string;
  name: string;
  body: string;
  shortcut: string;
  folder: string;
  scope: string;
};

type Macro = {
  id: string;
  name: string;
  body: string;
  actions: Array<{ id: string; type: string; params: Record<string, unknown> }>;
};

function expandReply(body: string, variables: { customerName: string; ticketNumber?: string }) {
  return body.replace(/\{\{\s*([^}]+?)\s*\}\}/g, (whole, key: string) => {
    switch (key.trim()) {
      case "customer.name":
        return variables.customerName;
      case "ticket.number":
        return variables.ticketNumber ?? "the ticket";
      default:
        return whole;
    }
  });
}
