import { ApiError, Breadcrumbs, Button, Card, CardBody, EmptyState, Textarea, api, formatDate, idempotencyKey, useMutation, useQuery } from "@hubchat/shared";
import { ArrowLeft, FileQuestion, MessageSquarePlus, ThumbsDown, ThumbsUp } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { usePortal } from "../portal-context";

type ArticleRecord = { id: string; title: string; slug: string; excerpt: string; body: string; helpful_count: number; unhelpful_count: number; updated_at: string };

export default function Article() {
  const { slug } = useParams();
  const { data: portal } = usePortal();
  const [vote, setVote] = useState<"up" | "down" | null>(null);
  const [comment, setComment] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const workspaceID = portal?.portal.workspace_id ?? "";
  const articleQuery = useQuery<ArticleRecord>(["portal-article", workspaceID, slug], (signal) => api.get(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/articles/${encodeURIComponent(slug ?? "")}`, { signal }), { enabled: Boolean(workspaceID && slug) });
  const feedback = useMutation<{ helpful: boolean; comment: string }, unknown>((input) => api.post(`/public/knowledge-bases/${encodeURIComponent(workspaceID)}/articles/${encodeURIComponent(slug ?? "")}/feedback`, input, { idempotencyKey: idempotencyKey() }), { onSuccess: () => setSubmitted(true) });
  const article = articleQuery.data;

  if (articleQuery.isLoading) return <p className="text-sm text-fg-muted">Loading article…</p>;
  if (!article || articleQuery.error) return <EmptyState icon={FileQuestion} size="lg" title="Article not found" description={articleQuery.error instanceof ApiError ? articleQuery.error.message : "It may have been moved or unpublished."} action={<Button variant="secondary" size="sm" asChild><Link to="/kb">Browse all guides</Link></Button>} />;

  return <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_220px]"><article className="min-w-0"><Breadcrumbs className="mb-4" items={[{ label: "Guides", href: "/kb" }, { label: article.title }]} /><h1 className="text-3xl font-semibold tracking-tighter text-fg">{article.title}</h1><p className="mt-2 text-xs text-fg-muted">Updated {formatDate(article.updated_at)}</p><div className="prose-portal mt-8 max-w-measure">{article.body.split("\n\n").map((block, index) => block.startsWith("## ") ? <h2 key={index}>{block.slice(3)}</h2> : block.startsWith("    ") ? <pre key={index}><code>{block.replace(/^ {4}/gm, "")}</code></pre> : <p key={index}>{block}</p>)}</div>
    <Card className="mt-12"><CardBody>{submitted ? <p className="text-sm text-fg">Thanks — that goes straight to whoever maintains this page.</p> : <><div className="flex flex-wrap items-center gap-3"><p className="text-sm text-fg">Was this helpful?</p><div className="flex gap-2"><Button variant={vote === "up" ? "primary" : "secondary"} size="sm" leading={<ThumbsUp />} onClick={() => setVote("up")}>Yes</Button><Button variant={vote === "down" ? "primary" : "secondary"} size="sm" leading={<ThumbsDown />} onClick={() => setVote("down")}>No</Button></div><p className="text-xs text-fg-muted sm:ml-auto">{article.helpful_count} people found this helpful</p></div>{vote && <div className="mt-4"><Textarea rows={3} value={comment} onChange={(event) => setComment(event.target.value)} placeholder={vote === "up" ? "Anything we should add? (optional)" : "What were you looking for?"} aria-label="Article feedback" /><div className="mt-2 flex justify-end"><Button variant="primary" size="sm" loading={feedback.isPending} onClick={() => void feedback.mutate({ helpful: vote === "up", comment }).catch(() => {})}>Send feedback</Button></div>{Boolean(feedback.error) && <p className="mt-2 text-xs text-danger">Could not record feedback. Please try again.</p>}</div>}</>}</CardBody></Card>
    <div className="mt-6 flex flex-wrap items-center gap-3"><Button variant="ghost" size="sm" leading={<ArrowLeft />} asChild><Link to="/kb">Back to guides</Link></Button><Button variant="secondary" size="sm" leading={<MessageSquarePlus />} asChild><Link to="/tickets/new">This did not answer my question</Link></Button></div></article></div>;
}
