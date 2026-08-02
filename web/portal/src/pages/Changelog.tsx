import { ApiError, Badge, Button, EmptyState, Pagination, cn, formatDate, api, useInfinite, type BadgeTone, type Paginated } from "@hubchat/shared";
import { Rss } from "lucide-react";
import { Link } from "react-router-dom";
import { usePortal } from "../portal-context";
import { portalText } from "../i18n";

const TAG_TONE: Record<string, BadgeTone> = {
  New: "accent",
  Improved: "info",
  Fixed: "neutral",
};

export default function Changelog() {
  const { data: portalData } = usePortal();
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portalData, key, fallback, values);
  const query = useInfinite<{ id: string; title: string; body: string; kind: string; published_at: string }>(
    ["portal-changelog", portalData?.portal.workspace_id ?? ""],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "25" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<{ id: string; title: string; body: string; kind: string; published_at: string }>>(`/public/changelog/${encodeURIComponent(portalData?.portal.workspace_id ?? "")}?${params.toString()}`, { signal });
    },
    { enabled: Boolean(portalData?.portal.workspace_id) },
  );
  const changelog = query.items;
  return (
    <div className="mx-auto max-w-2xl">
      <header className="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">{t("changelog", "Changelog")}</h1>
          <p className="mt-1.5 text-sm text-fg-muted">
            {t("changelog_description", "What we shipped, newest first.")}
          </p>
        </div>
        <Button variant="secondary" size="sm" leading={<Rss />} asChild>
          <Link to="/account">{t("subscription_settings", "Subscription settings")}</Link>
        </Button>
      </header>

      <ol className="relative">
        <span aria-hidden="true" className="absolute bottom-2 left-[5px] top-2 w-px bg-line" />

        {query.isLoading ? <p className="pl-7 text-sm text-fg-muted">{t("loading_updates", "Loading updates…")}</p> : query.error ? <EmptyState icon={Rss} title={t("changelog_unavailable", "Changelog unavailable")} description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} /> : changelog.length === 0 ? <EmptyState icon={Rss} title={t("no_updates", "No published updates yet")} description={t("check_updates", "Check back here for product updates.")} /> : changelog.map((entry) => (
          <li id={entry.id} key={entry.id} className="relative scroll-mt-20 pb-10 pl-7 last:pb-0">
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
      <Pagination hasPrevious={false} hasNext={query.hasMore} onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={t("updates_loaded", "{count} update{suffix} loaded", { count: changelog.length, suffix: changelog.length === 1 ? "" : "s" })} />
    </div>
  );
}
