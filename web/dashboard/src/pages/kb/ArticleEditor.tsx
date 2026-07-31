import {
  ApiError,
  Badge,
  Button,
  Callout,
  Field,
  Input,
  Page,
  Select,
  SegmentedControl,
  Textarea,
  Toolbar,
  ToolbarDivider,
  api,
  idempotencyKey,
  useMutation,
  useQuery,
  type Article,
  type ArticleCollection,
  type KnowledgeBase,
} from "@hubchat/shared";
import { Eye, History, Send } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

const SAMPLE = "# New article\n\nWrite the answer in Markdown. Published content is searchable in the portal and widget.";
type ArticleInput = { knowledge_base_id: string; collection_id?: string; title: string; slug: string; excerpt: string; body: string; state: string; language: string; seo: Record<string, string>; scheduled_at?: string | null };

function localDateTimeValue(value: string | null | undefined) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function utcTimestamp(value: string) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export default function ArticleEditor() {
  const { articleId } = useParams();
  const navigate = useNavigate();
  const isNew = !articleId || articleId === "new";
  const articleQuery = useQuery<Article>(["article", articleId], (signal) => api.get(`/articles/${encodeURIComponent(articleId ?? "")}`, { signal }), { enabled: !isNew });
  const basesQuery = useQuery<{ data: KnowledgeBase[] }>(["knowledge-bases"], (signal) => api.get("/knowledge-bases", { signal }));
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [title, setTitle] = useState("Untitled article");
  const [slug, setSlug] = useState("untitled-article");
  const [excerpt, setExcerpt] = useState("");
  const [body, setBody] = useState(SAMPLE);
  const [state, setState] = useState("draft");
  const [scheduleAt, setScheduleAt] = useState("");
  const [knowledgeBaseID, setKnowledgeBaseID] = useState("");
  const [collectionID, setCollectionID] = useState<string | undefined>();
  const collectionsQuery = useQuery<{ data: ArticleCollection[] }>(["collections", knowledgeBaseID], (signal) => api.get(`/knowledge-bases/${encodeURIComponent(knowledgeBaseID)}/collections`, { signal }), { enabled: Boolean(knowledgeBaseID) });

  useEffect(() => {
    const article = articleQuery.data;
    if (!article) return;
    setTitle(article.title); setSlug(article.slug); setExcerpt(article.excerpt); setBody(article.body); setState(article.state); setScheduleAt(localDateTimeValue(article.scheduled_at)); setKnowledgeBaseID(article.knowledge_base_id); setCollectionID(article.collection_id ?? undefined);
  }, [articleQuery.data]);
  useEffect(() => { if (!knowledgeBaseID && basesQuery.data?.data[0]) setKnowledgeBaseID(basesQuery.data.data[0].id); }, [basesQuery.data, knowledgeBaseID]);

  const save = useMutation<ArticleInput, Article>((input) => isNew ? api.post("/articles", input, { idempotencyKey: idempotencyKey() }) : api.patch(`/articles/${encodeURIComponent(articleId ?? "")}`, input), { onSuccess: (article) => { setState(article.state); setScheduleAt(localDateTimeValue(article.scheduled_at)); if (isNew) navigate(`/kb/articles/${article.id}`, { replace: true }); } });
  const publish = useMutation<void, Article>(() => api.post(`/articles/${encodeURIComponent(articleId ?? "")}/publish`, {}, { idempotencyKey: idempotencyKey() }), { onSuccess: () => { setState("published"); void articleQuery.refetch(); } });
  const input = (nextState: string): ArticleInput => ({ knowledge_base_id: knowledgeBaseID, collection_id: collectionID, title: title.trim(), slug: slug.trim().toLowerCase(), excerpt, body, state: nextState, language: "en", seo: {}, scheduled_at: nextState === "scheduled" ? utcTimestamp(scheduleAt) : null });

  if (articleQuery.isLoading || basesQuery.isLoading) return <Page><p className="p-6 text-sm text-fg-muted">Loading article editor…</p></Page>;
  if (articleQuery.error) return <Page><Callout tone="danger" className="m-6">{articleQuery.error instanceof ApiError ? articleQuery.error.message : "Could not load article."}</Callout></Page>;
  if (!knowledgeBaseID) return <Page><Callout tone="warning" className="m-6">Create a knowledge base before writing an article.</Callout></Page>;

  return (
    <Page>
      <Toolbar className="h-topbar py-0" leading={<><span className="truncate text-sm font-medium text-fg">{title}</span>{articleQuery.data && <Badge tone="neutral">v{articleQuery.data.version}</Badge>}</>} trailing={<><SegmentedControl aria-label="Editor mode" value={mode} onValueChange={setMode} options={[{ value: "write", label: "Write" }, { value: "preview", label: "Preview", icon: <Eye /> }]} /><ToolbarDivider /><Button variant="ghost" size="sm" leading={<History />}>History</Button><Button variant="secondary" size="sm" loading={save.isPending} disabled={!title.trim() || !slug.trim()} onClick={() => void save.mutate(input("draft")).catch(() => {})}>Save draft</Button><Button variant="secondary" size="sm" loading={save.isPending} disabled={!title.trim() || !slug.trim() || !scheduleAt || state === "published"} onClick={() => void save.mutate(input("scheduled")).catch(() => {})}>Schedule</Button><Button variant="primary" size="sm" leading={<Send />} loading={publish.isPending} disabled={isNew || state === "published"} onClick={() => void publish.mutate(undefined).catch(() => {})}>Publish now</Button></>} />
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col"><div className="min-h-0 flex-1 overflow-y-auto"><div className="mx-auto max-w-measure px-8 py-8"><Input value={title} onChange={(event) => { setTitle(event.target.value); if (isNew) setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")); }} aria-label="Article title" className="mb-4 border-0 bg-transparent text-3xl font-semibold tracking-tighter shadow-none" placeholder="Article title" />{mode === "write" ? <Textarea data-editor value={body} onChange={(event) => setBody(event.target.value)} aria-label="Article body" className="min-h-[60vh] resize-none border-0 bg-transparent p-0 font-mono text-sm leading-relaxed shadow-none" /> : <article className="prose-hubchat text-sm leading-relaxed text-fg-secondary">{body.split("\n\n").map((block, index) => block.startsWith("# ") ? <h1 key={index}>{block.slice(2)}</h1> : block.startsWith("## ") ? <h2 key={index}>{block.slice(3)}</h2> : block.startsWith("    ") ? <pre key={index}>{block.replace(/^ {4}/gm, "")}</pre> : <p key={index}>{block}</p>)}</article>}{Boolean(save.error) && <p className="mt-3 text-sm text-danger">{save.error instanceof ApiError ? save.error.message : "Could not save article."}</p>}</div></div></div>
        <aside className="hidden w-context shrink-0 overflow-y-auto border-l border-line bg-surface p-4 lg:block"><div className="space-y-4"><Field label="Knowledge base"><Select size="sm" value={knowledgeBaseID} onValueChange={setKnowledgeBaseID} options={(basesQuery.data?.data ?? []).map((base) => ({ value: base.id, label: base.name }))} /></Field><Field label="Collection"><Select size="sm" value={collectionID ?? ""} onValueChange={(value) => setCollectionID(value || undefined)} options={[{ value: "", label: "Uncategorised" }, ...(collectionsQuery.data?.data ?? []).map((collection) => ({ value: collection.id, label: collection.name }))]} /></Field><Field label="Slug" description="Changing this breaks existing links."><Input mono inputSize="sm" value={slug} onChange={(event) => setSlug(event.target.value)} /></Field><Field label="Excerpt"><Textarea rows={3} value={excerpt} onChange={(event) => setExcerpt(event.target.value)} placeholder="A short search result description" /></Field><Field label="Publish at" description="Uses your local time. The worker publishes the article when this time arrives."><Input type="datetime-local" inputSize="sm" value={scheduleAt} min={localDateTimeValue(new Date().toISOString())} onChange={(event) => setScheduleAt(event.target.value)} disabled={state === "published"} /></Field><Field label="State"><Badge tone={state === "published" ? "success" : state === "scheduled" ? "warning" : "neutral"}>{state.replace("_", " ")}</Badge></Field></div></aside>
      </div>
    </Page>
  );
}
