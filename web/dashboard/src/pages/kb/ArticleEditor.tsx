import {
  ApiError,
  Badge,
  Button,
  Callout,
  Dialog,
  DialogContent,
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
  useInfinite,
  useQuery,
  type Article,
  type ArticleCollection,
  type KnowledgeBase,
  type Paginated,
  formatDateTimeLocal,
  parseDateTimeLocal,
  formatDateTime,
} from "@hubchat/shared";
import { Eye, History, Send } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

const SAMPLE = "# New article\n\nWrite the answer in Markdown. Published content is searchable in the portal and widget.";
type ArticleInput = { knowledge_base_id: string; collection_id?: string; title: string; slug: string; excerpt: string; body: string; state: string; language: string; seo: Record<string, string>; scheduled_at?: string | null };
type ArticleRevision = { id: string; article_id: string; version: number; title: string; body: string; excerpt: string; edited_by?: string; note?: string; created_at: string };

export default function ArticleEditor() {
  const { workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const { articleId } = useParams();
  const navigate = useNavigate();
  const isNew = !articleId || articleId === "new";
  const articleQuery = useQuery<Article>(["article", articleId], (signal) => api.get(`/articles/${encodeURIComponent(articleId ?? "")}`, { signal }), { enabled: !isNew });
  const basesQuery = useInfinite<KnowledgeBase>(["knowledge-bases"], (cursor, signal) => api.get<Paginated<KnowledgeBase>>(`/knowledge-bases?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [title, setTitle] = useState("Untitled article");
  const [slug, setSlug] = useState("untitled-article");
  const [excerpt, setExcerpt] = useState("");
  const [body, setBody] = useState(SAMPLE);
  const [state, setState] = useState("draft");
  const [scheduleAt, setScheduleAt] = useState("");
  const [language, setLanguage] = useState("en");
  const [knowledgeBaseID, setKnowledgeBaseID] = useState("");
  const [collectionID, setCollectionID] = useState<string | undefined>();
  const [historyOpen, setHistoryOpen] = useState(false);
  const collectionsQuery = useInfinite<ArticleCollection>(knowledgeBaseID ? ["collections", knowledgeBaseID] : null, (cursor, signal) => api.get<Paginated<ArticleCollection>>(`/knowledge-bases/${encodeURIComponent(knowledgeBaseID)}/collections?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }), { enabled: Boolean(knowledgeBaseID) });
  const revisionsQuery = useInfinite<ArticleRevision>(articleId && !isNew && historyOpen ? ["article-revisions", articleId] : null, (cursor, signal) => {
    const params = new URLSearchParams({ limit: "25" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<ArticleRevision>>(`/articles/${encodeURIComponent(articleId ?? "")}/revisions?${params.toString()}`, { signal });
  }, { enabled: Boolean(articleId && !isNew && historyOpen) });

  useEffect(() => {
    const article = articleQuery.data;
    if (!article) return;
    setTitle(article.title); setSlug(article.slug); setExcerpt(article.excerpt); setBody(article.body); setState(article.state); setScheduleAt(article.scheduled_at ? formatDateTimeLocal(article.scheduled_at, workspace.timezone) : ""); setLanguage(article.language || "en"); setKnowledgeBaseID(article.knowledge_base_id); setCollectionID(article.collection_id ?? undefined);
  }, [articleQuery.data, workspace.timezone]);
  useEffect(() => { if (!knowledgeBaseID && basesQuery.items[0]) setKnowledgeBaseID(basesQuery.items[0].id); }, [basesQuery.items, knowledgeBaseID]);
  const activeBase = basesQuery.items.find((base) => base.id === knowledgeBaseID);
  const supportedLanguages = useMemo(() => activeBase?.languages?.length ? activeBase.languages : [activeBase?.default_language || "en"], [activeBase?.default_language, activeBase?.languages]);
  const languageOptions = useMemo(() => Array.from(new Set([...supportedLanguages, ...(!isNew && language ? [language] : [])])), [isNew, language, supportedLanguages]);
  useEffect(() => {
    if (!isNew || !activeBase || supportedLanguages.includes(language)) return;
    setLanguage(activeBase.default_language || supportedLanguages[0] || "en");
  }, [activeBase, isNew, language, supportedLanguages]);

  const save = useMutation<ArticleInput, Article>((input) => isNew ? api.post("/articles", input, { idempotencyKey: idempotencyKey() }) : api.patch(`/articles/${encodeURIComponent(articleId ?? "")}`, input), { onSuccess: (article) => { setState(article.state); setScheduleAt(article.scheduled_at ? formatDateTimeLocal(article.scheduled_at, workspace.timezone) : ""); if (isNew) navigate(`/kb/articles/${article.id}`, { replace: true }); } });
  const publish = useMutation<void, Article>(() => api.post(`/articles/${encodeURIComponent(articleId ?? "")}/publish`, {}, { idempotencyKey: idempotencyKey() }), { onSuccess: () => { setState("published"); void articleQuery.refetch(); } });
  const scheduleTimestamp = scheduleAt ? parseDateTimeLocal(scheduleAt, workspace.timezone) : null;
  const input = (nextState: string): ArticleInput => ({ knowledge_base_id: knowledgeBaseID, collection_id: collectionID, title: title.trim(), slug: slug.trim().toLowerCase(), excerpt, body, state: nextState, language, seo: {}, scheduled_at: nextState === "scheduled" ? scheduleTimestamp : null });

  if (articleQuery.isLoading || basesQuery.isLoading) return <Page><p className="p-6 text-sm text-fg-muted">Loading article editor…</p></Page>;
  if (articleQuery.error) return <Page><Callout tone="danger" className="m-6">{articleQuery.error instanceof ApiError ? articleQuery.error.message : "Could not load article."}</Callout></Page>;
  if (basesQuery.error) return <Page><Callout tone="danger" className="m-6">{basesQuery.error instanceof ApiError ? basesQuery.error.message : "Could not load knowledge bases."}</Callout></Page>;
  if (!knowledgeBaseID) return <Page><Callout tone="warning" className="m-6">Create a knowledge base before writing an article.</Callout></Page>;

  return (
    <Page>
      <Toolbar className="h-topbar py-0" leading={<><span className="truncate text-sm font-medium text-fg">{title}</span>{articleQuery.data && <Badge tone="neutral">v{articleQuery.data.version}</Badge>}</>} trailing={<><SegmentedControl aria-label="Editor mode" value={mode} onValueChange={setMode} options={[{ value: "write", label: "Write" }, { value: "preview", label: "Preview", icon: <Eye /> }]} /><ToolbarDivider /><Button variant="ghost" size="sm" leading={<History />} disabled={isNew} onClick={() => setHistoryOpen(true)}>History</Button><Button variant="secondary" size="sm" loading={save.isPending} disabled={!title.trim() || !slug.trim()} onClick={() => void save.mutate(input("draft")).catch(() => {})}>Save draft</Button><Button variant="secondary" size="sm" loading={save.isPending} disabled={!title.trim() || !slug.trim() || !scheduleAt || !scheduleTimestamp || state === "published"} onClick={() => void save.mutate(input("scheduled")).catch(() => {})}>Schedule</Button><Button variant="primary" size="sm" leading={<Send />} loading={publish.isPending} disabled={isNew || state === "published"} onClick={() => void publish.mutate(undefined).catch(() => {})}>Publish now</Button></>} />
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col"><div className="min-h-0 flex-1 overflow-y-auto"><div className="mx-auto max-w-measure px-8 py-8"><Input value={title} onChange={(event) => { setTitle(event.target.value); if (isNew) setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")); }} aria-label="Article title" className="mb-4 border-0 bg-transparent text-3xl font-semibold tracking-tighter shadow-none" placeholder="Article title" />{mode === "write" ? <Textarea data-editor value={body} onChange={(event) => setBody(event.target.value)} aria-label="Article body" className="min-h-[60vh] resize-none border-0 bg-transparent p-0 font-mono text-sm leading-relaxed shadow-none" /> : <article className="prose-hubchat text-sm leading-relaxed text-fg-secondary">{body.split("\n\n").map((block, index) => block.startsWith("# ") ? <h1 key={index}>{block.slice(2)}</h1> : block.startsWith("## ") ? <h2 key={index}>{block.slice(3)}</h2> : block.startsWith("    ") ? <pre key={index}>{block.replace(/^ {4}/gm, "")}</pre> : <p key={index}>{block}</p>)}</article>}{Boolean(save.error) && <p className="mt-3 text-sm text-danger">{save.error instanceof ApiError ? save.error.message : "Could not save article."}</p>}</div></div></div>
        <aside className="hidden w-context shrink-0 overflow-y-auto border-l border-line bg-surface p-4 lg:block"><div className="space-y-4"><Field label="Knowledge base"><Select size="sm" value={knowledgeBaseID} onValueChange={setKnowledgeBaseID} options={basesQuery.items.map((base) => ({ value: base.id, label: base.name }))} /></Field><Field label="Language" description="Choose the published language variant for this article."><Select size="sm" value={language} onValueChange={setLanguage} options={languageOptions.map((value) => ({ value, label: value }))} /></Field><Field label="Collection"><Select size="sm" value={collectionID ?? ""} onValueChange={(value) => setCollectionID(value || undefined)} options={[{ value: "", label: "Uncategorised" }, ...collectionsQuery.items.map((collection) => ({ value: collection.id, label: collection.name }))]} /></Field><Field label="Slug" description="Changing this breaks existing links."><Input mono inputSize="sm" value={slug} onChange={(event) => setSlug(event.target.value)} /></Field><Field label="Excerpt"><Textarea rows={3} value={excerpt} onChange={(event) => setExcerpt(event.target.value)} placeholder="A short search result description" /></Field><Field label="Publish at" description={`Uses ${workspace.timezone}. The worker publishes the article when this workspace time arrives.`}><Input type="datetime-local" inputSize="sm" value={scheduleAt} min={formatDateTimeLocal(new Date(), workspace.timezone)} onChange={(event) => setScheduleAt(event.target.value)} disabled={state === "published"} />{scheduleAt && !scheduleTimestamp && <p className="text-xs text-danger">That time does not exist in {workspace.timezone} because of a daylight-saving transition.</p>}</Field><Field label="State"><Badge tone={state === "published" ? "success" : state === "scheduled" ? "warning" : "neutral"}>{state.replace("_", " ")}</Badge></Field></div></aside>
      </div>
      {historyOpen && <Dialog open onOpenChange={setHistoryOpen}><DialogContent title="Article history" description="Append-only revisions saved by the article editor."><div className="space-y-3">{revisionsQuery.isLoading && <p className="text-sm text-fg-muted">Loading revisions…</p>}{Boolean(revisionsQuery.error) && <p className="text-sm text-danger">{revisionsQuery.error instanceof ApiError ? revisionsQuery.error.message : "Could not load article history."}</p>}{!revisionsQuery.isLoading && !revisionsQuery.error && revisionsQuery.items.length === 0 && <p className="text-sm text-fg-muted">No revisions recorded yet.</p>}{revisionsQuery.items.length > 0 && <ul className="divide-y divide-line rounded-md border border-line">{revisionsQuery.items.map((revision) => <li key={revision.id} className="space-y-1 p-3"><div className="flex items-center justify-between gap-3"><span className="text-sm font-medium text-fg">Version {revision.version}</span><time className="text-xs text-fg-muted" dateTime={revision.created_at}>{formatDateTime(revision.created_at, dateFormat)}</time></div><p className="truncate text-sm text-fg-secondary">{revision.title}</p><p className="text-xs text-fg-muted">{revision.note ?? "Saved revision"}</p></li>)}</ul>}{revisionsQuery.hasMore && <Button variant="ghost" size="sm" loading={revisionsQuery.isFetching} onClick={() => void revisionsQuery.fetchNext()}>Load more revisions</Button>}</div></DialogContent></Dialog>}
    </Page>
  );
}
