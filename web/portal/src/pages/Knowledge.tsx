import { Breadcrumbs, Card, EmptyState, SearchInput } from "@hubchat/shared";
import { ArrowRight, Book, FileQuestion } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { articles, collections } from "../data";

/** Article index, optionally scoped to one collection. */
export default function Knowledge() {
  const { collectionSlug } = useParams();
  const [query, setQuery] = useState("");

  const collection = collections.find((item) => item.slug === collectionSlug);

  const visible = articles
    .filter((article) => !collectionSlug || article.collection === collectionSlug)
    .filter((article) =>
      `${article.title} ${article.excerpt}`.toLowerCase().includes(query.toLowerCase()),
    );

  return (
    <div>
      <Breadcrumbs
        className="mb-4"
        items={[
          { label: "Help centre", href: "/" },
          { label: "Guides", href: "/kb" },
          ...(collection ? [{ label: collection.name }] : []),
        ]}
      />

      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tighter text-fg">
          {collection?.name ?? "All guides"}
        </h1>
        {collection?.description && (
          <p className="mt-1.5 text-sm text-fg-muted">{collection.description}</p>
        )}
      </header>

      <div className="mb-6 max-w-md">
        <SearchInput
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onClear={() => setQuery("")}
          placeholder={collection ? `Search ${collection.name.toLowerCase()}` : "Search all guides"}
        />
      </div>

      {!collectionSlug && !query && (
        <section className="mb-8">
          <div className="grid gap-3 sm:grid-cols-2">
            {collections.map((item) => (
              <Card key={item.slug} interactive className="p-0">
                <Link to={`/kb/${item.slug}`} className="flex items-center gap-3 p-4">
                  <Book aria-hidden="true" className="size-4 shrink-0 text-fg-muted" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-fg">{item.name}</span>
                    <span className="block text-xs text-fg-muted">{item.count} articles</span>
                  </span>
                  <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
                </Link>
              </Card>
            ))}
          </div>
        </section>
      )}

      {visible.length === 0 ? (
        <EmptyState
          icon={FileQuestion}
          title="No articles match"
          description="Try a different search, or send us a request and we will answer directly."
        />
      ) : (
        <ul className="space-y-2">
          {visible.map((article) => (
            <li key={article.slug}>
              <Card interactive className="p-0">
                <Link to={`/kb/article/${article.slug}`} className="block p-4">
                  <p className="text-sm font-medium text-fg">{article.title}</p>
                  <p className="mt-1 text-xs leading-normal text-fg-muted">{article.excerpt}</p>
                  <p className="mt-2 text-2xs text-fg-disabled">
                    {article.readingMinutes} min read · {article.helpful} people found this helpful
                  </p>
                </Link>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
