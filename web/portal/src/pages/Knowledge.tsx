import { ApiError, Breadcrumbs, Card, EmptyState, Pagination, SearchInput, api, useInfinite, type Paginated } from "@hubchat/shared";
import { ArrowRight, FileQuestion } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { portalLanguage, usePortal } from "../portal-context";
import { portalText } from "../i18n";

type Article = { id: string; slug: string; title: string; excerpt: string; view_count: number; helpful_count: number; updated_at: string };
/** Published knowledge-base search, scoped to the active portal workspace. */
export default function Knowledge() {
  const { collectionSlug } = useParams();
  const { data: portal } = usePortal();
  const [query, setQuery] = useState("");
  const workspaceID = portal?.portal.workspace_id ?? "";
  const language = portalLanguage(portal);
  const articles = useInfinite<{ article: Article }>(
    ["portal-knowledge", workspaceID, language, collectionSlug ?? "", query],
    (cursor, signal) => {
      const params = new URLSearchParams({ q: query, collection: collectionSlug ?? "", language, surface: "portal", limit: "25" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<{ article: Article }>>(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/search?${params.toString()}`, { signal });
    },
    { enabled: Boolean(workspaceID) },
  );
  const visible = articles.items.map((result) => result.article);
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portal, key, fallback, values);

  return (
    <div>
      <Breadcrumbs className="mb-4" items={[{ label: "Help centre", href: "/" }, { label: t("all_guides", "Guides") }]} />
      <header className="mb-6"><h1 className="text-2xl font-semibold tracking-tighter text-fg">{t("all_guides", "All guides")}</h1><p className="mt-1.5 text-sm text-fg-muted">{t("search_published_help", "Search the published help centre.")}</p></header>
      <div className="mb-6 max-w-md"><SearchInput value={query} onChange={(event) => setQuery(event.target.value)} onClear={() => setQuery("")} placeholder={t("search_all_guides", "Search all guides")} /></div>
      {articles.error ? <EmptyState icon={FileQuestion} title={t("help_centre_unavailable", "Help centre unavailable")} description={articles.error instanceof ApiError ? articles.error.message : "Try again in a moment."} /> : visible.length === 0 ? <EmptyState icon={FileQuestion} title={articles.isLoading ? t("loading_guides", "Loading guides…") : t("no_articles_match", "No articles match")} description="Try a different search, or send us a request and we will answer directly." /> : <ul className="space-y-2">{visible.map((article) => <li key={article.id}><Card interactive className="p-0"><Link to={`/kb/article/${article.slug}`} className="block p-4"><p className="text-sm font-medium text-fg">{article.title}</p><p className="mt-1 text-xs leading-normal text-fg-muted">{article.excerpt}</p><p className="mt-2 text-2xs text-fg-disabled">{article.helpful_count} people found this helpful</p></Link></Card></li>)}</ul>}
      <Pagination hasPrevious={false} hasNext={articles.hasMore} onPrevious={() => undefined} onNext={() => void articles.fetchNext()} summary={t("guides_loaded", "{count} guide{suffix} loaded", { count: visible.length, suffix: visible.length === 1 ? "" : "s" })} />
      {collectionSlug && <Link to="/kb" className="mt-5 inline-flex items-center gap-1 text-xs text-fg-muted hover:text-fg"><ArrowRight className="size-3" /> {t("browse_all_guides", "Browse all guides")}</Link>}
    </div>
  );
}
