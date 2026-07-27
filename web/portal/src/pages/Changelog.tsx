import { Badge, Button, cn, formatDate, type BadgeTone } from "@hubchat/shared";
import { Rss } from "lucide-react";
import { changelog } from "../data";

const TAG_TONE: Record<string, BadgeTone> = {
  New: "accent",
  Improved: "info",
  Fixed: "neutral",
};

export default function Changelog() {
  return (
    <div className="mx-auto max-w-2xl">
      <header className="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">Changelog</h1>
          <p className="mt-1.5 text-sm text-fg-muted">
            What we shipped, newest first.
          </p>
        </div>
        <Button variant="secondary" size="sm" leading={<Rss />}>
          Subscribe
        </Button>
      </header>

      <ol className="relative">
        <span aria-hidden="true" className="absolute bottom-2 left-[5px] top-2 w-px bg-line" />

        {changelog.map((entry) => (
          <li key={entry.version} className="relative pb-10 pl-7 last:pb-0">
            <span
              aria-hidden="true"
              className={cn(
                "absolute left-0 top-1.5 size-[11px] rounded-full border-2 border-canvas",
                "bg-accent",
              )}
            />

            <div className="flex flex-wrap items-center gap-2">
              <time className="font-mono text-xs text-fg-muted" dateTime={entry.date}>
                {formatDate(entry.date)}
              </time>
              <Badge tone="neutral" variant="outline">
                v{entry.version}
              </Badge>
              {entry.tags.map((tag) => (
                <Badge key={tag} tone={TAG_TONE[tag] ?? "neutral"}>
                  {tag}
                </Badge>
              ))}
            </div>

            <h2 className="mt-2 text-lg font-semibold tracking-tight text-fg">{entry.title}</h2>
            <p className="mt-2 text-sm leading-relaxed text-fg-secondary">{entry.body}</p>
          </li>
        ))}
      </ol>
    </div>
  );
}
