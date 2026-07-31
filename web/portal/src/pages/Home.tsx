import { Badge, Card, SearchInput, formatRelativeShort } from "@hubchat/shared";
import {
  ArrowRight,
  Book,
  Code2,
  Layout,
  Lightbulb,
  MessageSquarePlus,
  Receipt,
  Rocket,
  TicketCheck,
} from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { NOW, articles, collections, portal, tickets, viewer } from "../data";

const ICONS: Record<string, typeof Book> = {
  rocket: Rocket,
  layout: Layout,
  code: Code2,
  receipt: Receipt,
};

/**
 * Portal home.
 *
 * Search is the primary control and gets the visual weight to match — most
 * people arrive knowing roughly what they want. Browsing and contact options
 * sit below it for the people who do not.
 */
export default function Home() {
  const [query, setQuery] = useState("");

  const results = query
    ? articles.filter((article) =>
        `${article.title} ${article.excerpt}`.toLowerCase().includes(query.toLowerCase()),
      )
    : [];

  const openTickets = tickets.filter((ticket) => ticket.status !== "resolved");

  return (
    <>
      {/* Hero ------------------------------------------------------------ */}
      <section className="relative overflow-hidden border-b border-line">
        <div
          aria-hidden="true"
          className="absolute inset-0"
          style={{
            background: `radial-gradient(80% 120% at 50% -20%, color-mix(in oklab, ${portal.accent} 16%, transparent), transparent 70%)`,
          }}
        />
        <div aria-hidden="true" className="hc-grid-bg absolute inset-0 opacity-40" />

        <div className="relative mx-auto max-w-3xl px-4 py-16 text-center sm:px-6 sm:py-20">
          <h1 className="text-3xl font-semibold tracking-tighter text-fg sm:text-4xl">
            {portal.headline}
          </h1>
          <p className="mx-auto mt-3 max-w-xl text-md leading-normal text-fg-muted">
            {portal.subheadline}
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
                {results.length === 0 ? (
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
                      <li key={article.slug}>
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
        {/* Your requests ------------------------------------------------- */}
        {viewer.signedIn && openTickets.length > 0 && (
          <section className="mb-10">
            <div className="mb-3 flex items-baseline justify-between gap-4">
              <h2 className="text-md font-semibold tracking-tight text-fg">Your open requests</h2>
              <Link to="/tickets" className="text-xs text-accent-text hover:underline">
                View all
              </Link>
            </div>

            <div className="space-y-2">
              {openTickets.map((ticket) => (
                <Card key={ticket.number} interactive className="p-0">
                  <Link to={`/tickets/${ticket.number}`} className="flex items-center gap-3 p-4">
                    <TicketCheck aria-hidden="true" className="size-4 shrink-0 text-fg-muted" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm text-fg">{ticket.title}</span>
                      <span className="block text-xs text-fg-muted">
                        {ticket.number} · updated {formatRelativeShort(ticket.updated, NOW)} ago
                      </span>
                    </span>
                    <Badge tone={ticket.status === "open" ? "accent" : "neutral"}>
                      {ticket.status === "on_hold" ? "On hold" : "Open"}
                    </Badge>
                  </Link>
                </Card>
              ))}
            </div>
          </section>
        )}

        {/* Collections ---------------------------------------------------- */}
        <section className="mb-10">
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">Browse guides</h2>

          <div className="grid gap-3 sm:grid-cols-2">
            {collections.map((collection) => {
              const Icon = ICONS[collection.icon] ?? Book;
              return (
                <Card key={collection.slug} interactive className="p-0">
                  <Link to={`/kb/${collection.slug}`} className="flex items-start gap-3 p-4">
                    <span
                      aria-hidden="true"
                      className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent-subtle"
                    >
                      <Icon className="size-4 text-accent-text" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm font-medium text-fg">{collection.name}</span>
                      <span className="mt-0.5 block text-xs leading-normal text-fg-muted">
                        {collection.description}
                      </span>
                      <span className="mt-1.5 block text-2xs text-fg-disabled">
                        {collection.count} articles
                      </span>
                    </span>
                  </Link>
                </Card>
              );
            })}
          </div>
        </section>

        {/* Popular -------------------------------------------------------- */}
        <section className="mb-10">
          <h2 className="mb-3 text-md font-semibold tracking-tight text-fg">Most read</h2>
          <Card>
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {articles.slice(0, 4).map((article) => (
                  <li key={article.slug}>
                    <Link
                      to={`/kb/article/${article.slug}`}
                      className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"
                    >
                      <Book aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate text-sm text-fg">
                        {article.title}
                      </span>
                      <span className="shrink-0 text-2xs text-fg-muted">
                        {article.readingMinutes} min
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
              <Link to="/tickets/new" className="block p-4">
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
