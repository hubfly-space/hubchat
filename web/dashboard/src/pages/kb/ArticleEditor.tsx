import {
  ApiError, Badge, Button, Callout, Dialog, DialogContent, Field, Input, Markdown,
  Page, Select, SegmentedControl, Textarea, Toolbar, ToolbarDivider, api,
  idempotencyKey, useMutation, useInfinite, useQuery, setQueryData, invalidate,
  type Article, type ArticleCollection, type KnowledgeBase, type Paginated,
  formatDateTimeLocal, parseDateTimeLocal, formatDateTime,
} from "@hubchat/shared";
import { Eye, History, Send } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

type Draft = {
  title: string; slug: string; excerpt: string; body: string; language: string;
  knowledge_base_id: string; collection_id?: string; seo: Record<string, unknown>;
  schedule: string;
};
type Revision = { id: string; version: number; title: string; body: string; excerpt: string; note?: string; created_at: string };
const initialDraft: Draft = { title: "Untitled article", slug: "untitled-article", excerpt: "", body: "# New article\n\nWrite the answer in Markdown.", language: "en", knowledge_base_id: "", seo: {}, schedule: "" };
const fingerprint = (draft: Draft) => JSON.stringify(draft);

export default function ArticleEditor() {
  const { workspace, viewer } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const { articleId } = useParams();
  const navigate = useNavigate();
  const isNew = !articleId || articleId === "new";
  const storageKey = `hubchat.article-draft:${viewer.id}:${workspace.id}:${articleId ?? "new"}`;
  const articleQuery = useQuery<Article>(["article", articleId], (signal) => api.get(`/articles/${encodeURIComponent(articleId ?? "")}`, { signal }), { enabled: !isNew });
  const bases = useInfinite<KnowledgeBase>(["knowledge-bases"], (cursor, signal) => api.get<Paginated<KnowledgeBase>>(`/knowledge-bases?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [draft, setDraft] = useState<Draft>(initialDraft);
  const [baseline, setBaseline] = useState<string | null>(null);
  const [articleState, setArticleState] = useState("draft");
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [historyOpen, setHistoryOpen] = useState(false);
  const [selectedRevision, setSelectedRevision] = useState<Revision | null>(null);
  const [conflict, setConflict] = useState(false);
  const [recovered, setRecovered] = useState(false);
  const version = useRef<number | undefined>(undefined);
  const loadedId = useRef<string | undefined>(undefined);
  const liveDraft = useRef(draft);
  liveDraft.current = draft;
  const dirty = baseline !== null && fingerprint(draft) !== baseline;
  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((current) => ({ ...current, [key]: value }));
  const collections = useInfinite<ArticleCollection>(draft.knowledge_base_id ? ["collections", draft.knowledge_base_id] : null, (cursor, signal) => api.get<Paginated<ArticleCollection>>(`/knowledge-bases/${encodeURIComponent(draft.knowledge_base_id)}/collections?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const revisions = useInfinite<Revision>(historyOpen && !isNew ? ["article-revisions", articleId] : null, (cursor, signal) => api.get<Paginated<Revision>>(`/articles/${articleId}/revisions?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));

  useEffect(() => {
    const article = articleQuery.data;
    if (!article || (loadedId.current === article.id && (dirty || article.version <= (version.current ?? 0)))) return;
    loadedId.current = article.id;
    version.current = article.version;
    const remote: Draft = { title: article.title, slug: article.slug, excerpt: article.excerpt, body: article.body, language: article.language, knowledge_base_id: article.knowledge_base_id, collection_id: article.collection_id ?? undefined, seo: article.seo, schedule: article.scheduled_at ? formatDateTimeLocal(article.scheduled_at, workspace.timezone) : "" };
    setBaseline(fingerprint(remote));
    setArticleState(article.state);
    try {
      const stored = JSON.parse(sessionStorage.getItem(storageKey) ?? "null") as { draft?: Draft; version?: number } | null;
      if (stored?.draft && typeof stored.draft.body === "string") { setDraft(stored.draft); setRecovered(true); if(stored.version!==article.version){version.current=stored.version;setConflict(true);} return; }
    } catch { /* Storage is optional. */ }
    setDraft(remote);
  }, [articleQuery.data, storageKey, workspace.timezone,dirty]);

  useEffect(() => {
    if (!isNew || !bases.items[0] || baseline !== null) return;
    const first = bases.items[0];
    const next = { ...initialDraft, knowledge_base_id: first.id, language: first.default_language || "en" };
    setDraft(next); setBaseline(fingerprint(next));
  }, [isNew, bases.items, baseline]);

  useEffect(() => {
    if (!dirty) return;
    try { sessionStorage.setItem(storageKey, JSON.stringify({ draft, version: version.current })); } catch { /* Private browsing. */ }
  }, [dirty, draft, storageKey]);

  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    const beforeNavigate = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest("a[href]") as HTMLAnchorElement | null;
      if (link && link.origin === location.origin && link.pathname !== location.pathname && !window.confirm("Leave this article with unsaved edits? A recovery copy is kept in this tab.")) { event.preventDefault(); event.stopPropagation(); }
    };
    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", beforeNavigate, true);
    return () => { window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("click", beforeNavigate, true); };
  }, [dirty]);

  const save = useMutation<{ draft: Draft; state: string }, Article>(async ({ draft: snapshot, state }) => {
    const scheduled = snapshot.schedule ? parseDateTimeLocal(snapshot.schedule, workspace.timezone) : null;
    const { schedule: _schedule, ...fields } = snapshot;
    const input = { ...fields, title: snapshot.title.trim(), slug: snapshot.slug.trim().toLowerCase(), state, scheduled_at: state === "scheduled" ? scheduled : null, expected_version: version.current };
    return isNew ? api.post("/articles", input, { idempotencyKey: idempotencyKey() }) : api.patch(`/articles/${articleId}`, input, { idempotencyKey: idempotencyKey() });
  }, {
    onSuccess: (article, submitted) => {
      version.current = article.version;
      setBaseline(fingerprint(submitted.draft));
      setArticleState(article.state);
      setConflict(false); setRecovered(false);
      setQueryData(["article", article.id], article);
      invalidate(["articles"]); invalidate(["article-revisions", article.id]);
      // Only discard the recovery copy when no edits arrived during the save.
      if (fingerprint(liveDraft.current) === fingerprint(submitted.draft)) { try { sessionStorage.removeItem(storageKey); } catch { /* optional */ } }
      if (isNew) {
        if (fingerprint(liveDraft.current)!==fingerprint(submitted.draft)) { try { sessionStorage.setItem(`hubchat.article-draft:${viewer.id}:${workspace.id}:${article.id}`,JSON.stringify({draft:liveDraft.current,version:article.version})); } catch { /* optional */ } }
        navigate(`/kb/articles/${article.id}`, { replace: true });
      }
    },
    onError: (error) => { if (error instanceof ApiError && error.status === 409) setConflict(true); },
  });
  const submit = (state: string) => { if (!save.isPending) void save.mutate({ draft: { ...draft }, state }).catch(() => {}); };
  const autosave = save.mutate;
  const valid = Boolean(draft.knowledge_base_id && draft.title.trim() && draft.slug.trim());
  useEffect(() => {
    // Autosave unpublished drafts only: editing a published article must not
    // silently change the public answer or unpublish it.
    if (isNew || !dirty || !valid || save.isPending || save.error || conflict || articleState !== "draft") return;
    const timer = window.setTimeout(() => { void autosave({ draft: { ...draft }, state: "draft" }).catch(() => {}); }, 1500);
    return () => window.clearTimeout(timer);
  }, [isNew, dirty, valid, save.isPending, save.error, autosave, conflict, articleState, draft]);

  const activeBase = bases.items.find((base) => base.id === draft.knowledge_base_id);
  const languages = useMemo(() => [...new Set([...(activeBase?.languages ?? ["en"]), draft.language])], [activeBase?.languages, draft.language]);
  const reload = async () => {
    if (dirty && !window.confirm("Discard your local edits and load the latest saved version?")) return;
    const latest = await api.get<Article>(`/articles/${articleId}`, { fresh: true });
    loadedId.current = undefined; setConflict(false); save.reset();
    try { sessionStorage.removeItem(storageKey); } catch { /* optional */ }
    setQueryData(["article", articleId], latest);
  };

  const keepLocal = async () => {
    const latest = await api.get<Article>(`/articles/${articleId}`, { fresh: true });
    if(!window.confirm("Replace the latest saved version with your local edits? Review its history first if needed."))return;
    version.current=latest.version;setArticleState(latest.state);setConflict(false);save.reset();
  };

  if (articleQuery.isLoading || bases.isLoading) return <Page><p role="status" className="p-6 text-sm text-fg-muted">Loading article editor…</p></Page>;
  if (articleQuery.error || bases.error) return <Page><Callout tone="danger" className="m-6">Could not load the editor. <Button onClick={() => { articleQuery.refetch(); bases.refetch(); }}>Retry</Button></Callout></Page>;
  if (!draft.knowledge_base_id) return <Page><Callout tone="warning" className="m-6">Create a knowledge base before writing an article.</Callout></Page>;

  return <Page>
    <Toolbar className="h-auto min-h-topbar flex-wrap py-2" leading={<div><span className="text-sm font-medium">{draft.title}</span><span role="status" className="ml-3 text-xs text-fg-muted">{save.isPending ? "Saving…" : conflict ? "Conflict — edits preserved" : dirty ? articleState === "draft" && !isNew ? "Unsaved · autosave pending" : "Unsaved changes" : "Saved"}</span></div>} trailing={<>
      <SegmentedControl aria-label="Editor mode" value={mode} onValueChange={setMode} options={[{ value: "write", label: "Write" }, { value: "preview", label: "Preview", icon: <Eye /> }]} />
      <ToolbarDivider /><Button variant="ghost" size="sm" leading={<History />} disabled={isNew} onClick={() => setHistoryOpen(true)}>History</Button>
      <Button variant="secondary" size="sm" loading={save.isPending} disabled={!valid || conflict} onClick={() => submit("draft")}>Save draft</Button>
      <Button variant="secondary" size="sm" disabled={!valid || !draft.schedule || !parseDateTimeLocal(draft.schedule, workspace.timezone) || save.isPending || conflict} onClick={() => submit("scheduled")}>Schedule</Button>
      <Button size="sm" leading={<Send />} loading={save.isPending} disabled={!valid || conflict} onClick={() => submit("published")}>Publish current edits</Button>
    </>} />
    {recovered && <Callout tone="info" className="m-4">Recovered your unsaved edits from this tab.</Callout>}
    {Boolean(save.error) && <Callout tone="danger" className="m-4">{save.error instanceof ApiError ? save.error.message : "Could not save your article."} {conflict ? <><Button variant="secondary" size="sm" onClick={() => void reload()}>Load latest version</Button><Button variant="ghost" size="sm" onClick={()=>setHistoryOpen(true)}>Review history</Button><Button variant="ghost" size="sm" onClick={()=>void keepLocal()}>Keep local edits</Button></> : <Button variant="secondary" size="sm" onClick={() => { save.reset(); submit(articleState); }}>Retry save</Button>}</Callout>}
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto lg:flex-row">
      <div className="min-w-0 flex-1"><div className="mx-auto max-w-measure p-4 sm:p-8">
        <Input value={draft.title} onChange={(event) => { const value = event.target.value; setDraft((current) => ({ ...current, title: value, ...(isNew ? { slug: value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") } : {}) })); }} aria-label="Article title" className="mb-4 text-2xl font-semibold" />
        {mode === "write" ? <Textarea data-editor value={draft.body} onChange={(event) => patch("body", event.target.value)} aria-label="Article body" className="min-h-[60vh] font-mono text-sm" /> : <Markdown>{draft.body}</Markdown>}
      </div></div>
      <aside className="w-full shrink-0 border-t border-line bg-surface p-4 lg:w-context lg:border-l lg:border-t-0"><div className="space-y-4">
        <Field label="Knowledge base"><Select value={draft.knowledge_base_id} onValueChange={(value) => { patch("knowledge_base_id", value); patch("collection_id", undefined); }} options={bases.items.map((base) => ({ value: base.id, label: base.name }))} /></Field>
        <Field label="Language"><Select value={draft.language} onValueChange={(value) => patch("language", value)} options={languages.map((value) => ({ value, label: value }))} /></Field>
        <Field label="Collection"><Select value={draft.collection_id ?? ""} onValueChange={(value) => patch("collection_id", value || undefined)} options={[{ value: "", label: "Uncategorised" }, ...collections.items.map((collection) => ({ value: collection.id, label: collection.name }))]} /></Field>
        <Field label="Slug"><Input value={draft.slug} onChange={(event) => patch("slug", event.target.value)} /></Field>
        <Field label="Excerpt"><Textarea rows={3} value={draft.excerpt} onChange={(event) => patch("excerpt", event.target.value)} /></Field>
        <Field label="SEO title"><Input value={String(draft.seo.title ?? "")} onChange={(event) => patch("seo", { ...draft.seo, title: event.target.value })} /></Field>
        <Field label="SEO description"><Textarea rows={3} value={String(draft.seo.description ?? "")} onChange={(event) => patch("seo", { ...draft.seo, description: event.target.value })} /></Field>
        <Field label="Publish at" description={`Uses ${workspace.timezone}.`}><Input type="datetime-local" value={draft.schedule} onChange={(event) => patch("schedule", event.target.value)} /></Field>
        <Badge tone={articleState === "published" ? "success" : "neutral"}>{articleState} · v{version.current ?? 1}</Badge>
        {articleState === "published" && <p className="text-xs text-fg-muted">Autosave pauses for published articles. Publish current edits updates the public answer; Save draft unpublishes it.</p>}
      </div></aside>
    </div>
    <Dialog open={historyOpen} onOpenChange={setHistoryOpen}><DialogContent title="Article history" description="Compare a revision and restore its content into the editor. Publishing remains a separate action.">
      {revisions.isLoading && <p role="status">Loading revisions…</p>}
      {Boolean(revisions.error) && <Callout tone="danger">Could not load history. <Button onClick={revisions.refetch}>Retry</Button></Callout>}
      <div className="max-h-[60vh] space-y-3 overflow-y-auto">
        {revisions.items.map((revision) => <div key={revision.id} className="rounded border border-line p-3"><div className="flex items-center justify-between gap-3"><span>Version {revision.version}</span><time className="text-xs text-fg-muted">{formatDateTime(revision.created_at, dateFormat)}</time></div><p className="text-sm">{revision.title}</p><Button variant="ghost" size="sm" onClick={() => setSelectedRevision(revision)}>Compare</Button><Button variant="secondary" size="sm" onClick={() => { setDraft((current) => ({ ...current, title: revision.title, body: revision.body, excerpt: revision.excerpt })); setHistoryOpen(false); }}>Use this version</Button></div>)}
        {selectedRevision && <div><p className="mb-2 text-sm">Version {selectedRevision.version} compared with your current editor</p><div className="grid gap-2 sm:grid-cols-2"><pre className="whitespace-pre-wrap rounded border border-line p-3 text-xs">{selectedRevision.body}</pre><pre className="whitespace-pre-wrap rounded border border-line p-3 text-xs">{draft.body}</pre></div></div>}
        {revisions.hasMore && <Button onClick={() => void revisions.fetchNext()}>Load more revisions</Button>}
      </div>
    </DialogContent></Dialog>
  </Page>;
}
