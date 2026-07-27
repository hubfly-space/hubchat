import {
  Breadcrumbs,
  Button,
  Card,
  CardBody,
  EmptyState,
  Textarea,
  cn,
  formatDate,
} from "@hubchat/shared";
import { ArrowLeft, FileQuestion, MessageSquarePlus, ThumbsDown, ThumbsUp } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { articles, collections } from "../data";

export default function Article() {
  const { slug } = useParams();
  const [vote, setVote] = useState<"up" | "down" | null>(null);
  const [comment, setComment] = useState("");
  const [submitted, setSubmitted] = useState(false);

  const article = articles.find((item) => item.slug === slug);

  if (!article) {
    return (
      <EmptyState
        icon={FileQuestion}
        size="lg"
        title="Article not found"
        description="It may have been moved or unpublished."
        action={
          <Button variant="secondary" size="sm" asChild>
            <Link to="/kb">Browse all guides</Link>
          </Button>
        }
      />
    );
  }

  const collection = collections.find((item) => item.slug === article.collection);
  const related = articles
    .filter((item) => item.collection === article.collection && item.slug !== article.slug)
    .slice(0, 3);

  return (
    <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_220px]">
      <article className="min-w-0">
        <Breadcrumbs
          className="mb-4"
          items={[
            { label: "Guides", href: "/kb" },
            ...(collection ? [{ label: collection.name, href: `/kb/${collection.slug}` }] : []),
            { label: article.title },
          ]}
        />

        <h1 className="text-3xl font-semibold tracking-tighter text-fg">{article.title}</h1>
        <p className="mt-2 text-xs text-fg-muted">
          Updated {formatDate(article.updated)} · {article.readingMinutes} min read
        </p>

        <div className="prose-portal mt-8 max-w-measure">
          {article.body.split("\n\n").map((block, index) => {
            if (block.startsWith("## ")) {
              return <h2 key={index}>{block.slice(3)}</h2>;
            }
            if (block.startsWith("    ")) {
              return (
                <pre key={index}>
                  <code>{block.replace(/^ {4}/gm, "")}</code>
                </pre>
              );
            }
            return <p key={index}>{block}</p>;
          })}
        </div>

        {/* Article feedback --------------------------------------------- */}
        <Card className="mt-12">
          <CardBody>
            {submitted ? (
              <p className="text-sm text-fg">
                Thanks — that goes straight to whoever maintains this page.
              </p>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-3">
                  <p className="text-sm text-fg">Was this helpful?</p>
                  <div className="flex gap-2">
                    <Button
                      variant={vote === "up" ? "primary" : "secondary"}
                      size="sm"
                      leading={<ThumbsUp />}
                      onClick={() => setVote("up")}
                    >
                      Yes
                    </Button>
                    <Button
                      variant={vote === "down" ? "primary" : "secondary"}
                      size="sm"
                      leading={<ThumbsDown />}
                      onClick={() => setVote("down")}
                    >
                      No
                    </Button>
                  </div>
                  <p className="text-xs text-fg-muted sm:ml-auto">
                    {article.helpful} people found this helpful
                  </p>
                </div>

                {vote && (
                  <div className="mt-4 animate-fade-up">
                    <Textarea
                      rows={3}
                      value={comment}
                      onChange={(event) => setComment(event.target.value)}
                      placeholder={
                        vote === "up"
                          ? "Anything we should add? (optional)"
                          : "What were you looking for?"
                      }
                      aria-label="Article feedback"
                    />
                    <div className="mt-2 flex justify-end">
                      <Button variant="primary" size="sm" onClick={() => setSubmitted(true)}>
                        Send feedback
                      </Button>
                    </div>
                  </div>
                )}
              </>
            )}
          </CardBody>
        </Card>

        <div className="mt-6 flex flex-wrap items-center gap-3">
          <Button variant="ghost" size="sm" leading={<ArrowLeft />} asChild>
            <Link to={collection ? `/kb/${collection.slug}` : "/kb"}>Back to guides</Link>
          </Button>
          <Button variant="secondary" size="sm" leading={<MessageSquarePlus />} asChild>
            <Link to="/tickets/new">This did not answer my question</Link>
          </Button>
        </div>
      </article>

      {/* Related ---------------------------------------------------------- */}
      <aside className="min-w-0">
        <div className={cn("lg:sticky lg:top-20")}>
          <p className="mb-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
            Related
          </p>
          <ul className="space-y-1">
            {related.map((item) => (
              <li key={item.slug}>
                <Link
                  to={`/kb/article/${item.slug}`}
                  className="-mx-2 block rounded-md px-2 py-1.5 text-xs leading-normal text-fg-secondary transition-colors hover:bg-fill hover:text-fg"
                >
                  {item.title}
                </Link>
              </li>
            ))}
            {related.length === 0 && (
              <li className="text-xs text-fg-muted">Nothing else in this collection yet.</li>
            )}
          </ul>
        </div>
      </aside>
    </div>
  );
}
