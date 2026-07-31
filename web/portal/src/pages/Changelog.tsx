import { ApiError, Badge, Button, EmptyState, cn, formatDate, api, useQuery, type BadgeTone } from "@hubchat/shared";
import { Rss } from "lucide-react";
import { usePortal } from "../portal-context";

const TAG_TONE: Record<string, BadgeTone> = {
  New: "accent",
  Improved: "info",
  Fixed: "neutral",
};

export default function Changelog() {
  const { data: portalData } = usePortal();
  const query = useQuery<{ data: Array<{ id: string; title: string; body: string; kind: string; published_at: string }> }>(
    ["portal-changelog", portalData?.portal.workspace_id ?? ""],
    (signal) => api.get(`/public/changelog/${encodeURIComponent(portalData?.portal.workspace_id ?? "")}`, { signal }),
    { enabled: Boolean(portalData?.portal.workspace_id) },
  );
  const changelog = query.data?.data ?? [];
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

        {query.isLoading ? <p className="pl-7 text-sm text-fg-muted">Loading updates…</p> : query.isError ? <EmptyState icon={Rss} title="Changelog unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} /> : changelog.length === 0 ? <EmptyState icon={Rss} title="No published updates yet" description="Check back here for product updates." /> : changelog.map((entry) => (
          <li key={entry.id} className="relative pb-10 pl-7 last:pb-0">
            <span
              aria-hidden="true"
              className={cn(
                "absolute left-0 top-1.5 size-[11px] rounded-full border-2 border-canvas",
                "bg-accent",
              )}
            />

            <div className="flex flex-wrap items-center gap-2">
              <time className="font-mono text-xs text-fg-muted" dateTime={entry.published_at}>
                {formatDate(entry.published_at)}
              </time>
              <Badge tone={TAG_TONE[entry.kind] ?? "neutral"}>{entry.kind}</Badge>
            </div>

            <h2 className="mt-2 text-lg font-semibold tracking-tight text-fg">{entry.title}</h2>
            <p className="mt-2 text-sm leading-relaxed text-fg-secondary">{entry.body}</p>
          </li>
        ))}
      </ol>
    </div>
  );
}
