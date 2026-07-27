import { Check, Copy } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { useCopyToClipboard } from "../lib/hooks";
import { Button } from "./Button";
import { Tooltip } from "./Tooltip";

export function InlineCode({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <code
      className={cn(
        "rounded-xs border border-line-subtle bg-inset px-1 py-px font-mono text-[0.9em] text-fg-secondary",
        className,
      )}
    >
      {children}
    </code>
  );
}

export type CodeBlockProps = {
  code: string;
  /** Shown in the header strip. Not used for highlighting. */
  language?: string;
  filename?: string;
  /** Line numbers help when docs reference a specific line. */
  showLineNumbers?: boolean;
  /** Caps the height and scrolls. Use for payloads and logs. */
  maxHeight?: number;
  actions?: ReactNode;
  className?: string;
};

/**
 * No syntax highlighter. Install snippets, webhook payloads, and SDK examples
 * are short, and a highlighting library would be the single largest dependency
 * in the dashboard bundle. Structure comes from the mono face and the surface
 * treatment instead.
 */
export function CodeBlock({
  code,
  language,
  filename,
  showLineNumbers,
  maxHeight,
  actions,
  className,
}: CodeBlockProps) {
  const { copied, copy } = useCopyToClipboard();
  const lines = code.split("\n");

  return (
    <div className={cn("overflow-hidden rounded-md border border-line bg-inset", className)}>
      {(filename || language || actions) && (
        <div className="flex items-center justify-between gap-2 border-b border-line px-3 py-1.5">
          <span className="truncate font-mono text-2xs text-fg-muted">
            {filename ?? language}
          </span>
          <div className="flex shrink-0 items-center gap-1">
            {actions}
            <Tooltip content={copied ? "Copied" : "Copy"}>
              <Button
                variant="ghost"
                size="xs"
                iconOnly
                aria-label="Copy code"
                onClick={() => void copy(code)}
                leading={copied ? <Check className="text-success-text" /> : <Copy />}
              />
            </Tooltip>
          </div>
        </div>
      )}

      <div className="overflow-auto" style={maxHeight ? { maxHeight } : undefined}>
        <pre className="p-3 font-mono text-xs leading-relaxed text-fg-secondary">
          <code>
            {showLineNumbers
              ? lines.map((line, index) => (
                  <span key={index} className="grid grid-cols-[2.5ch_1fr] gap-3">
                    <span className="select-none text-right text-fg-disabled tabular">
                      {index + 1}
                    </span>
                    <span>{line || " "}</span>
                  </span>
                ))
              : code}
          </code>
        </pre>
      </div>
    </div>
  );
}

/**
 * A value the user needs to copy exactly: a widget public key, a workspace id,
 * an API key prefix, a webhook signing secret hint.
 */
export function CopyField({
  value,
  label,
  masked,
  className,
}: {
  value: string;
  label?: string;
  /** Renders dots and reveals only on explicit action (§12 sensitive fields). */
  masked?: boolean;
  className?: string;
}) {
  const { copied, copy } = useCopyToClipboard();

  return (
    <div
      className={cn(
        "flex items-center gap-1 rounded-md border border-line bg-inset pl-2.5 pr-1",
        className,
      )}
    >
      {label && <span className="shrink-0 text-xs text-fg-muted">{label}</span>}
      <code className="min-w-0 flex-1 truncate py-1.5 font-mono text-xs text-fg-secondary">
        {masked ? "•".repeat(Math.min(value.length, 28)) : value}
      </code>
      <Tooltip content={copied ? "Copied" : "Copy"}>
        <Button
          variant="ghost"
          size="xs"
          iconOnly
          aria-label={`Copy ${label ?? "value"}`}
          onClick={() => void copy(value)}
          leading={copied ? <Check className="text-success-text" /> : <Copy />}
        />
      </Tooltip>
    </div>
  );
}
