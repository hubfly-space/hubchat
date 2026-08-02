import { ApiError, Card, CardBody, EmptyState, SearchInput, api, useQuery, type Paginated } from "@hubchat/shared";
import { ArrowRight, Book, Lightbulb, MessageSquarePlus } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { portalAccent, portalFeatureEnabled, portalLanguage, portalThemeText, usePortal } from "../portal-context";
import { portalText } from "../i18n";

type Article = { id: string; slug: string; title: string; excerpt: string; updated_at: string; view_count: number };
type SearchResponse = Paginated<{ article: Article }>;

/**
 * Portal home.
 *
 * Search is the primary control and gets the visual weight to match — most
 * people arrive knowing roughly what they want. Browsing and contact options
 * sit below it for the people who do not.
 */
export default function Home() {
  const [query, setQuery] = useState("");
  const { data: portalData } = usePortal();
  const accent = portalAccent(portalData?.portal);
  const workspaceID = portalData?.portal.workspace_id ?? "";
  const ticketsEnabled = portalFeatureEnabled(portalData?.portal, "tickets");
  const knowledgeBaseEnabled = portalFeatureEnabled(portalData?.portal, "knowledge_base");
  const feedbackEnabled = portalFeatureEnabled(portalData?.portal, "feedback");
  const language = portalLanguage(portalData);
  const articles = useQuery<SearchResponse>(
    ["portal-home-knowledge", workspaceID, language, query],
    (signal) => api.get(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/search?q=${encodeURIComponent(query)}&language=${encodeURIComponent(language)}&surface=portal`, { signal }),
    { enabled: Boolean(workspaceID && knowledgeBaseEnabled) },
  );
  const results = (articles.data?.data ?? []).map((item) => item.article);
  const navigation = portalData?.portal.navigation ?? [];
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portalData, key, fallback, values);

  return (
    <>
      {/* Hero ------------------------------------------------------------ */}
      <section className="relative overflow-hidden border-b border-line">
        <div
          aria-hidden="true"
          className="absolute inset-0"
          style={{
            background: `radial-gradient(80% 120% at 50% -20%, color-mix(in oklab, ${accent} 16%, transparent), transparent 70%)`,
          }}
        />
        <div aria-hidden="true" className="hc-grid-bg absolute inset-0 opacity-40" />

        <div className="relative mx-auto max-w-3xl px-4 py-16 text-center sm:px-6 sm:py-20">
          <h1 className="text-3xl font-semibold tracking-tighter text-fg sm:text-4xl">
            {portalThemeText(portalData?.portal, "headline", "How can we help?")}
          </h1>
          <p className="mx-auto mt-3 max-w-xl text-md leading-normal text-fg-muted">
            {portalThemeText(portalData?.portal, "subheadline", "Search our guides, track your requests, or start a conversation with the team.")}
          </p>

          {knowledgeBaseEnabled ? <div className="relative mx-auto mt-7 max-w-xl">
            <SearchInput
              inputSize="lg"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onClear={() => setQuery("")}
              placeholder={t("search_guides", "Search guides and answers…")}
              aria-label={t("search_help_centre", "Search the help centre")}
              className="shadow-2"
            />

            {query && (
              <div className="absolute inset-x-0 top-full z-10 mt-2 overflow-hidden rounded-lg border border-line bg-overlay text-left shadow-3">
                {articles.isError ? (
                  <div className="px-4 py-6 text-center text-sm text-danger">
                    {articles.error instanceof ApiError ? articles.error.message : t("help_centre_unavailable", "Help centre unavailable.")}
                  </div>
                ) : results.length === 0 ? (
                  <div className="px-4 py-6 text-center">
                    <p className="text-sm text-fg">{t("nothing_matches", "Nothing matches “{query}”", { query })}</p>
                    <p className="mt-1 text-xs text-fg-muted">
                      {t("try_fewer_words", "Try fewer words, or")} {" "}
                      {ticketsEnabled ? <Link to="/tickets/new" className="text-accent-text hover:underline">
                        {t("send_us_request", "send us a request")}
                      </Link> : t("contact_team", "contact the team")}
                      .
                    </p>
                  </div>
                ) : (
                  <ul className="max-h-80 overflow-y-auto p-1">
                {results.map((article) => (
                      <li key={article.id}>
                        <Link
                          to={`/kb/article/${article.slug}`}
                          className="block rounded-md px-3 py-2 transition-colors hover:bg-fill"
                        >
                          <p className="text-sm text-fg">{article.title}</p>
                          <p className="mt-0.5 line-clamp-1 text-xs text-fg-muted">
                            {article.excerpt}
                          </p>
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div> : <p className="mx-auto mt-7 max-w-xl text-sm text-fg-muted">{t("use_links_support", "Use the links above to find support or contact the team.")}</p>}
        </div>
      </section>

      <div className="mx-auto w-full max-w-5xl px-4 py-10 sm:px-6">
        {knowledgeBaseEnabled && <section className="mb-10">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h2 className="text-md font-semibold tracking-tight text-fg">{t("latest_guides", "Latest guides")}</h2>
            <Link to="/kb" className="text-xs text-accent-text hover:underline">{t("browse_all", "Browse all")}</Link>
          </div>
          {articles.isLoading ? <p className="text-sm text-fg-muted">{t("loading_guides", "Loading guides…")}</p> : articles.isError ? <EmptyState icon={Book} title={t("guides_unavailable", "Guides unavailable")} description="Try again in a moment or send us a request." /> : results.length === 0 ? <EmptyState icon={Book} title={t("no_guides", "No published guides yet")} description="Send us a request and we will help directly." /> : (
            <Card><CardBody className="p-0"><ul className="divide-y divide-line-subtle">{results.slice(0, 6).map((article) => <li key={article.id}><Link to={`/kb/article/${article.slug}`} className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"><Book aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" /><span className="min-w-0 flex-1"><span className="block truncate text-sm text-fg">{article.title}</span><span className="mt-0.5 block line-clamp-1 text-xs text-fg-muted">{article.excerpt}</span></span><ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" /></Link></li>)}</ul></CardBody></Card>
          )}
        </section>}

        {/* Popular -------------------------------------------------------- */}
        {knowledgeBaseEnabled && <section className="mb-10">
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">{t("most_read", "Most read")}</h2>
          <Card>
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {results.slice(0, 4).map((article) => (
                  <li key={article.id}>
                    <Link
                      to={`/kb/article/${article.slug}`}
                      className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"
                    >
                      <Book aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate text-sm text-fg">
                        {article.title}
                      </span>
                      <span className="shrink-0 text-2xs text-fg-muted">
                        {t("views", "{count} views", { count: article.view_count })}
                      </span>
                      <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
                    </Link>
                  </li>
                ))}
              </ul>
            </CardBody>
          </Card>
        </section>}

        {/* Contact -------------------------------------------------------- */}
        {(ticketsEnabled || feedbackEnabled) && <section>
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">{t("still_need_help", "Still need help?")}</h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {ticketsEnabled && <Card interactive className="p-0">
              <Link to={navigation.find((item) => !item.external && item.href === "/tickets/new")?.href ?? "/tickets/new"} className="block p-4">
                <MessageSquarePlus aria-hidden="true" className="size-4 text-accent-text" />
                <p className="mt-2.5 text-sm font-medium text-fg">{t("send_request", "Send a request")}</p>
                <p className="mt-1 text-xs leading-normal text-fg-muted">
                  {t("reply_business_day", "We reply within one business day, usually much sooner.")}
                </p>
              </Link>
            </Card>}

            {feedbackEnabled && <Card interactive className="p-0">
              <Link to="/feedback" className="block p-4">
                <Lightbulb aria-hidden="true" className="size-4 text-accent-text" />
                <p className="mt-2.5 text-sm font-medium text-fg">{t("suggest_improvement", "Suggest an improvement")}</p>
                <p className="mt-1 text-xs leading-normal text-fg-muted">
                  {t("vote_build", "Vote on what we build next, or add something we have missed.")}
                </p>
              </Link>
            </Card>}
          </div>
        </section>}
      </div>
    </>
  );
}
