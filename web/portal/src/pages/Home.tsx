import { ApiError, Card, CardBody, EmptyState, SearchInput, api, useQuery } from "@hubchat/shared";
import { ArrowRight, Book, Lightbulb, MessageSquarePlus } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { portalAccent, usePortal } from "../portal-context";

type Article = { id: string; slug: string; title: string; excerpt: string; updated_at: string; view_count: number };
type SearchResponse = { data: Array<{ article: Article }> };

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
  const articles = useQuery<SearchResponse>(
    ["portal-home-knowledge", workspaceID, query],
    (signal) => api.get(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/search?q=${encodeURIComponent(query)}&surface=portal`, { signal }),
    { enabled: Boolean(workspaceID) },
  );
  const results = (articles.data?.data ?? []).map((item) => item.article);
  const navigation = portalData?.portal.navigation ?? [];

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
            How can we help?
          </h1>
          <p className="mx-auto mt-3 max-w-xl text-md leading-normal text-fg-muted">
            Search our guides, track your requests, or start a conversation with the team.
          </p>

          <div className="relative mx-auto mt-7 max-w-xl">
            <SearchInput
              inputSize="lg"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onClear={() => setQuery("")}
              placeholder="Search guides and answers…"
              aria-label="Search the help centre"
              className="shadow-2"
            />

            {query && (
              <div className="absolute inset-x-0 top-full z-10 mt-2 overflow-hidden rounded-lg border border-line bg-overlay text-left shadow-3">
                {articles.isError ? (
                  <div className="px-4 py-6 text-center text-sm text-danger">
                    {articles.error instanceof ApiError ? articles.error.message : "Help centre unavailable."}
                  </div>
                ) : results.length === 0 ? (
                  <div className="px-4 py-6 text-center">
                    <p className="text-sm text-fg">Nothing matches “{query}”</p>
                    <p className="mt-1 text-xs text-fg-muted">
                      Try fewer words, or{" "}
                      <Link to="/tickets/new" className="text-accent-text hover:underline">
                        send us a request
                      </Link>
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
          </div>
        </div>
      </section>

      <div className="mx-auto w-full max-w-5xl px-4 py-10 sm:px-6">
        <section className="mb-10">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h2 className="text-md font-semibold tracking-tight text-fg">Latest guides</h2>
            <Link to="/kb" className="text-xs text-accent-text hover:underline">Browse all</Link>
          </div>
          {articles.isLoading ? <p className="text-sm text-fg-muted">Loading guides…</p> : articles.isError ? <EmptyState icon={Book} title="Guides unavailable" description="Try again in a moment or send us a request." /> : results.length === 0 ? <EmptyState icon={Book} title="No published guides yet" description="Send us a request and we will help directly." /> : (
            <Card><CardBody className="p-0"><ul className="divide-y divide-line-subtle">{results.slice(0, 6).map((article) => <li key={article.id}><Link to={`/kb/article/${article.slug}`} className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"><Book aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" /><span className="min-w-0 flex-1"><span className="block truncate text-sm text-fg">{article.title}</span><span className="mt-0.5 block line-clamp-1 text-xs text-fg-muted">{article.excerpt}</span></span><ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" /></Link></li>)}</ul></CardBody></Card>
          )}
        </section>

        {/* Popular -------------------------------------------------------- */}
        <section className="mb-10">
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">Most read</h2>
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
                        {article.view_count} views
                      </span>
                      <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
                    </Link>
                  </li>
                ))}
              </ul>
            </CardBody>
          </Card>
        </section>

        {/* Contact -------------------------------------------------------- */}
        <section>
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">Still need help?</h2>
          <div className="grid gap-3 sm:grid-cols-2">
            <Card interactive className="p-0">
              <Link to={navigation.find((item) => item.href === "/tickets/new")?.href ?? "/tickets/new"} className="block p-4">
                <MessageSquarePlus aria-hidden="true" className="size-4 text-accent-text" />
                <p className="mt-2.5 text-sm font-medium text-fg">Send a request</p>
                <p className="mt-1 text-xs leading-normal text-fg-muted">
                  We reply within one business day, usually much sooner.
                </p>
              </Link>
            </Card>

            <Card interactive className="p-0">
              <Link to="/feedback" className="block p-4">
                <Lightbulb aria-hidden="true" className="size-4 text-accent-text" />
                <p className="mt-2.5 text-sm font-medium text-fg">Suggest an improvement</p>
                <p className="mt-1 text-xs leading-normal text-fg-muted">
                  Vote on what we build next, or add something we have missed.
                </p>
              </Link>
            </Card>
          </div>
        </section>
      </div>
    </>
  );
}
