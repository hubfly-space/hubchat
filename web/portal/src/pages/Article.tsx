import { ApiError, Breadcrumbs, Button, Card, CardBody, EmptyState, Textarea, api, formatDate, idempotencyKey, useMutation, useQuery } from "@hubchat/shared";
import { ArrowLeft, FileQuestion, MessageSquarePlus, ThumbsDown, ThumbsUp } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { portalLanguage, usePortal } from "../portal-context";
import { portalText } from "../i18n";

type ArticleRecord = { id: string; title: string; slug: string; excerpt: string; body: string; helpful_count: number; unhelpful_count: number; updated_at: string };

export default function Article() {
  const { slug } = useParams();
  const { data: portal } = usePortal();
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portal, key, fallback, values);
  const [vote, setVote] = useState<"up" | "down" | null>(null);
  const [comment, setComment] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const workspaceID = portal?.portal.workspace_id ?? "";
  const language = portalLanguage(portal);
  const articlePath = `/public/knowledge-bases/${encodeURIComponent(workspaceID)}/articles/${encodeURIComponent(slug ?? "")}`;
  const articleQuery = useQuery<ArticleRecord>(["portal-article", workspaceID, language, slug], (signal) => api.get(`${articlePath}?language=${encodeURIComponent(language)}`, { signal }), { enabled: Boolean(workspaceID && slug) });
  const feedback = useMutation<{ helpful: boolean; comment: string }, unknown>((input) => api.post(`${articlePath}/feedback?language=${encodeURIComponent(language)}`, { ...input, language }, { idempotencyKey: idempotencyKey() }), { onSuccess: () => setSubmitted(true) });
  const article = articleQuery.data;

  if (articleQuery.isLoading) return <p className="text-sm text-fg-muted">{t("loading_article", "Loading article…")}</p>;
  if (!article || articleQuery.error) return <EmptyState icon={FileQuestion} size="lg" title={t("article_not_found", "Article not found")} description={articleQuery.error instanceof ApiError ? articleQuery.error.message : t("article_unpublished", "It may have been moved or unpublished.")} action={<Button variant="secondary" size="sm" asChild><Link to="/kb">{t("browse_all_guides", "Browse all guides")}</Link></Button>} />;

  return <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_220px]"><article className="min-w-0"><Breadcrumbs className="mb-4" items={[{ label: t("all_guides", "Guides"), href: "/kb" }, { label: article.title }]} /><h1 className="text-3xl font-semibold tracking-tighter text-fg">{article.title}</h1><p className="mt-2 text-xs text-fg-muted">{t("updated_date", "Updated {date}", { date: formatDate(article.updated_at) })}</p><div className="prose-portal mt-8 max-w-measure">{article.body.split("\n\n").map((block, index) => block.startsWith("## ") ? <h2 key={index}>{block.slice(3)}</h2> : block.startsWith("    ") ? <pre key={index}><code>{block.replace(/^ {4}/gm, "")}</code></pre> : <p key={index}>{block}</p>)}</div>
    <Card className="mt-12"><CardBody>{submitted ? <p className="text-sm text-fg">{t("article_feedback_thanks", "Thanks — that goes straight to whoever maintains this page.")}</p> : <><div className="flex flex-wrap items-center gap-3"><p className="text-sm text-fg">{t("was_helpful", "Was this helpful?")}</p><div className="flex gap-2"><Button variant={vote === "up" ? "primary" : "secondary"} size="sm" leading={<ThumbsUp />} onClick={() => setVote("up")}>{t("yes", "Yes")}</Button><Button variant={vote === "down" ? "primary" : "secondary"} size="sm" leading={<ThumbsDown />} onClick={() => setVote("down")}>{t("no", "No")}</Button></div><p className="text-xs text-fg-muted sm:ml-auto">{t("helpful_count", "{count} people found this helpful", { count: article.helpful_count })}</p></div>{vote && <div className="mt-4"><Textarea rows={3} value={comment} onChange={(event) => setComment(event.target.value)} placeholder={vote === "up" ? t("anything_add", "Anything we should add? (optional)") : t("what_looking", "What were you looking for?")} aria-label={t("article_feedback", "Article feedback")} /><div className="mt-2 flex justify-end"><Button variant="primary" size="sm" loading={feedback.isPending} onClick={() => void feedback.mutate({ helpful: vote === "up", comment }).catch(() => {})}>{t("send_feedback", "Send feedback")}</Button></div>{Boolean(feedback.error) && <p className="mt-2 text-xs text-danger">{t("feedback_error", "Could not record feedback. Please try again.")}</p>}</div>}</>}</CardBody></Card>
    <div className="mt-6 flex flex-wrap items-center gap-3"><Button variant="ghost" size="sm" leading={<ArrowLeft />} asChild><Link to="/kb">{t("back_to_guides", "Back to guides")}</Link></Button><Button variant="secondary" size="sm" leading={<MessageSquarePlus />} asChild><Link to="/tickets/new">{t("article_not_answer", "This did not answer my question")}</Link></Button></div></article></div>;
}
