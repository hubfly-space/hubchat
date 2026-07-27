import {
  Button,
  Kbd,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Tabs,
  TabsList,
  Textarea,
  Tooltip,
  cn,
  useToast,
} from "@hubchat/shared";
import {
  Bold,
  CalendarClock,
  Italic,
  Link2,
  List,
  Lock,
  MessageSquare,
  Paperclip,
  Send,
  Smile,
  Zap,
} from "lucide-react";
import { useRef, useState } from "react";
import { macros, savedReplies } from "../../data/fixtures";

type ComposerMode = "reply" | "note";

export type ComposerProps = {
  customerName: string;
  /**
   * When provided, Send calls the real backend instead of simulating one.
   * Wired today by pages/dev/LiveDemo.tsx, which talks to the actual Go API
   * (web/dashboard/src/lib/api.ts) — the rest of the dashboard is still
   * fixture-driven (see data/fixtures.ts), so most call sites omit this and
   * get the local demo behaviour below.
   */
  onSend?: (body: string, kind: ComposerMode) => Promise<void>;
};

/**
 * Agent composer (§6.2).
 *
 * The mode switch is the most consequential control on the screen, so it is not
 * a subtle toggle: switching to a note re-tints the entire composer amber and
 * relabels the send button. There is no state in which "am I about to reply
 * publicly?" requires a second look.
 */
export function Composer({ customerName, onSend }: ComposerProps) {
  const [mode, setMode] = useState<ComposerMode>("reply");
  const [value, setValue] = useState("");
  const [sending, setSending] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const toast = useToast();

  const isNote = mode === "note";
  const canSend = value.trim().length > 0 && !sending;

  const send = () => {
    if (!canSend) return;

    if (onSend) {
      const body = value;
      setSending(true);
      onSend(body, mode)
        .then(() => {
          setValue("");
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
      return;
    }

    toast.success({
      title: isNote ? "Note added" : "Reply sent",
      description: isNote ? "Only your team can see this." : `Delivered to ${customerName}.`,
      action: { label: "Undo", onClick: () => setValue(value) },
    });
    setValue("");
  };

  const insert = (snippet: string) => {
    setValue((current) => (current ? `${current}\n\n${snippet}` : snippet));
    textareaRef.current?.focus();
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

        <div className="flex items-center justify-between gap-2 border-t border-line-subtle px-2 py-1.5">
          <div className="flex items-center gap-0.5">
            <Tooltip content="Bold" shortcut="mod+b">
              <Button variant="ghost" size="xs" iconOnly aria-label="Bold" leading={<Bold />} />
            </Tooltip>
            <Tooltip content="Italic" shortcut="mod+i">
              <Button variant="ghost" size="xs" iconOnly aria-label="Italic" leading={<Italic />} />
            </Tooltip>
            <Tooltip content="Link" shortcut="mod+shift+k">
              <Button variant="ghost" size="xs" iconOnly aria-label="Insert link" leading={<Link2 />} />
            </Tooltip>
            <Tooltip content="List">
              <Button variant="ghost" size="xs" iconOnly aria-label="Bulleted list" leading={<List />} />
            </Tooltip>

            <span className="mx-1 h-4 w-px bg-line" aria-hidden="true" />

            <Tooltip content="Attach file">
              <Button variant="ghost" size="xs" iconOnly aria-label="Attach file" leading={<Paperclip />} />
            </Tooltip>
            <Tooltip content="Emoji">
              <Button variant="ghost" size="xs" iconOnly aria-label="Insert emoji" leading={<Smile />} />
            </Tooltip>

            <Menu>
              <Tooltip content="Saved replies and macros" shortcut="mod+/">
                <MenuTrigger asChild>
                  <Button variant="ghost" size="xs" iconOnly aria-label="Saved replies" leading={<Zap />} />
                </MenuTrigger>
              </Tooltip>
              <MenuContent align="start" side="top" className="w-72">
                <MenuLabel>Saved replies</MenuLabel>
                {savedReplies.map((reply) => (
                  <MenuItem
                    key={reply.id}
                    onSelect={() => insert(reply.body)}
                    description={reply.body}
                    shortcut={reply.shortcut ?? undefined}
                  >
                    {reply.name}
                  </MenuItem>
                ))}
                <MenuLabel>Macros</MenuLabel>
                {macros.map((macro) => (
                  <MenuItem
                    key={macro.id}
                    icon={<Zap />}
                    onSelect={() => macro.body && insert(macro.body)}
                    description={`${macro.actions.length} action${macro.actions.length === 1 ? "" : "s"} · used ${macro.usage_count.toLocaleString()}×`}
                  >
                    {macro.name}
                  </MenuItem>
                ))}
              </MenuContent>
            </Menu>
          </div>

          <div className="flex items-center gap-1.5">
            {!isNote && (
              <Tooltip content="Send later">
                <Button variant="ghost" size="xs" iconOnly aria-label="Schedule send" leading={<CalendarClock />} />
              </Tooltip>
            )}

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
