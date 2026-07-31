import {
  ApiError,
  Badge,
  Button,
  DataTable,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  SearchInput,
  SegmentedControl,
  Toolbar,
  Tooltip,
  QueryBoundary,
  api,
  formatCompact,
  formatRelativeShort,
  useQuery,
  type Article,
  type ArticleState,
  type BadgeTone,
  type Column,
} from "@hubchat/shared";
import { FileText, Plus, ThumbsDown, ThumbsUp } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

const STATE: Record<ArticleState, { label: string; tone: BadgeTone }> = {
  draft: { label: "Draft", tone: "neutral" },
  in_review: { label: "In review", tone: "warning" },
  scheduled: { label: "Scheduled", tone: "info" },
  published: { label: "Published", tone: "success" },
  archived: { label: "Archived", tone: "neutral" },
};

/** Knowledge-base article index (§6.8). */
export default function ArticleList() {
  const navigate = useNavigate();
  const { memberById } = useWorkspace();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<"all" | ArticleState>("all");

  const articlesQuery = useQuery<{ data: Article[] }>(
    ["articles", filter, query],
    (signal) => api.get(`/articles?state=${encodeURIComponent(filter === "all" ? "" : filter)}&q=${encodeURIComponent(query)}&limit=200`, { signal }),
  );
  const rows = articlesQuery.data?.data ?? [];

  const columns: Column<Article>[] = [
    {
      key: "title",
      header: "Article",
      cell: (article) => {
        return (
          <div className="min-w-0">
            <p className="truncate text-sm text-fg">{article.title}</p>
            <p className="truncate text-xs text-fg-muted">
              /{article.slug}
            </p>
          </div>
        );
      },
      sortable: true,
    },
    {
      key: "state",
      header: "State",
      width: "112px",
      cell: (article) => <Badge tone={STATE[article.state].tone}>{STATE[article.state].label}</Badge>,
      sortable: true,
    },
    {
      key: "author",
      header: "Author",
      width: "150px",
      hideBelow: "lg",
      cell: (article) => (
        <span className="text-xs text-fg-secondary">{memberById(article.author_id)?.name ?? "—"}</span>
      ),
    },
    {
      key: "view_count",
      header: "Views",
      width: "84px",
      numeric: true,
      cell: (article) => formatCompact(article.view_count),
      sortable: true,
    },
    {
      key: "helpfulness",
      header: "Helpful",
      width: "116px",
      hideBelow: "md",
      cell: (article) => {
        const total = article.helpful_count + article.unhelpful_count;
        if (total === 0) return <span className="text-xs text-fg-disabled">No votes</span>;
        const ratio = article.helpful_count / total;
        return (
          <Tooltip content={`${article.helpful_count} helpful · ${article.unhelpful_count} not helpful`}>
            <span className="flex items-center gap-1.5 text-xs tabular">
              {ratio >= 0.8 ? (
                <ThumbsUp aria-hidden="true" className="size-3 text-success-text" />
              ) : (
                <ThumbsDown aria-hidden="true" className="size-3 text-warning-text" />
              )}
              {Math.round(ratio * 100)}%
            </span>
          </Tooltip>
        );
      },
    },
    {
      key: "updated_at",
      header: "Updated",
      width: "92px",
      numeric: true,
      cell: (article) => (
        <span className="text-xs text-fg-muted">{formatRelativeShort(article.updated_at)}</span>
      ),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Articles"
        description="Self-service content served through the portal and searched inside the widget."
        actions={
          <Button
            variant="primary"
            size="sm"
            leading={<Plus />}
            onClick={() => navigate("/kb/articles/new")}
          >
            New article
          </Button>
        }
      />

      <Toolbar
        leading={
          <>
            <div className="w-64">
              <SearchInput
                inputSize="sm"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onClear={() => setQuery("")}
                placeholder="Search articles"
              />
            </div>
            <SegmentedControl
              aria-label="Filter by state"
              value={filter}
              onValueChange={setFilter}
              options={[
                { value: "all", label: "All" },
                { value: "published", label: "Published" },
                { value: "draft", label: "Drafts" },
                { value: "in_review", label: "In review" },
              ]}
            />
          </>
        }
      />

      <PageBody>
        <QueryBoundary query={articlesQuery}>
          {() => (
            <div className="min-h-0 flex-1 overflow-auto">
              <DataTable
                aria-label="Articles"
                rows={rows}
                columns={columns}
                rowKey={(article) => article.id}
                onRowClick={(article) => navigate(`/kb/articles/${article.id}`)}
                empty={
                  <EmptyState
                    icon={FileText}
                    title="No articles here"
                    description="A good first article answers the question your team types out most often."
                    action={
                      <Button variant="primary" size="sm" leading={<Plus />} onClick={() => navigate("/kb/articles/new")}>
                        Write one
                      </Button>
                    }
                  />
                }
              />
              {articlesQuery.error instanceof ApiError && <p className="mt-3 text-sm text-danger">{articlesQuery.error.message}</p>}
            </div>
          )}
        </QueryBoundary>
      </PageBody>

      <Pagination
        hasPrevious={false}
        hasNext={false}
        onPrevious={() => undefined}
        onNext={() => undefined}
        summary={`${rows.length} article${rows.length === 1 ? "" : "s"}`}
      />
    </Page>
  );
}
