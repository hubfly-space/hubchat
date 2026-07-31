import { ApiError, Breadcrumbs, Card, EmptyState, SearchInput, api, useQuery } from "@hubchat/shared";
import { ArrowRight, FileQuestion } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { usePortal } from "../portal-context";

type Article = { id: string; slug: string; title: string; excerpt: string; view_count: number; helpful_count: number; updated_at: string };
type SearchResponse = { data: Array<{ article: Article }> };

/** Published knowledge-base search, scoped to the active portal workspace. */
export default function Knowledge() {
  const { collectionSlug } = useParams();
  const { data: portal } = usePortal();
  const [query, setQuery] = useState("");
  const workspaceID = portal?.portal.workspace_id ?? "";
  const articles = useQuery<SearchResponse>(
    ["portal-knowledge", workspaceID, collectionSlug ?? "", query],
    (signal) => api.get(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/search?q=${encodeURIComponent(query)}&surface=portal`, { signal }),
    { enabled: Boolean(workspaceID) },
  );
  const visible = (articles.data?.data ?? []).map((result) => result.article);

  return (
    <div>
      <Breadcrumbs className="mb-4" items={[{ label: "Help centre", href: "/" }, { label: "Guides" }]} />
      <header className="mb-6"><h1 className="text-2xl font-semibold tracking-tighter text-fg">All guides</h1><p className="mt-1.5 text-sm text-fg-muted">Search the published help centre.</p></header>
      <div className="mb-6 max-w-md"><SearchInput value={query} onChange={(event) => setQuery(event.target.value)} onClear={() => setQuery("")} placeholder="Search all guides" /></div>
      {articles.isError ? <EmptyState icon={FileQuestion} title="Help centre unavailable" description={articles.error instanceof ApiError ? articles.error.message : "Try again in a moment."} /> : visible.length === 0 ? <EmptyState icon={FileQuestion} title={articles.isLoading ? "Loading guides…" : "No articles match"} description="Try a different search, or send us a request and we will answer directly." /> : <ul className="space-y-2">{visible.map((article) => <li key={article.id}><Card interactive className="p-0"><Link to={`/kb/article/${article.slug}`} className="block p-4"><p className="text-sm font-medium text-fg">{article.title}</p><p className="mt-1 text-xs leading-normal text-fg-muted">{article.excerpt}</p><p className="mt-2 text-2xs text-fg-disabled">{article.helpful_count} people found this helpful</p></Link></Card></li>)}</ul>}
      {collectionSlug && <Link to="/kb" className="mt-5 inline-flex items-center gap-1 text-xs text-fg-muted hover:text-fg"><ArrowRight className="size-3" /> Browse all guides</Link>}
    </div>
  );
}
