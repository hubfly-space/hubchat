import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Section,
  Tabs,
  TabsContent,
  TabsList,
  api,
  idempotencyKey,
  useInfinite,
  useMutation,
  useQuery,
  type Paginated,
} from "@hubchat/shared";
import { Download, FileArchive, RefreshCw, Upload } from "lucide-react";
import { useRef, useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

type ExportRequest = {
  id: string;
  kind: string;
  state: "pending" | "running" | "completed" | "failed" | "expired";
  file_id?: string;
  row_count?: number;
  error?: string;
  expires_at?: string;
  completed_at?: string;
  created_at: string;
};

type ImportRequest = {
  id: string;
  kind: string;
  state: "pending" | "validating" | "running" | "completed" | "failed" | "cancelled";
  file_id?: string;
  total_rows?: number;
  processed_rows: number;
  failed_rows: number;
  error?: string;
  created_at: string;
};

type PreviewSummary = { name: string; rows: number; existing?: number; new?: number };
type ExportManifest = {
  export_id: string;
  file_id: string;
  file_name: string;
  size_bytes: number;
  checksum: string;
  expires_at?: string;
  row_count: number;
  attachment_count: number;
  attachment_bytes: number;
  tables: Array<{ name: string; rows: number }>;
};

const statusTone = (state: string): "neutral" | "info" | "success" | "warning" | "danger" => {
  if (state === "completed") return "success";
  if (state === "failed" || state === "expired" || state === "cancelled") return "danger";
  if (state === "running" || state === "validating") return "info";
  return "neutral";
};

function statusLabel(state: string): string {
  return state.replaceAll("_", " ").replace(/^./, (character) => character.toUpperCase());
}

function displayDate(value?: string): string {
  if (!value) return "—";
  return new Date(value).toLocaleString();
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

/** Import, export, and portability (§6.20). */
export default function ImportExport() {
  const [tab, setTab] = useState("export");
  const [downloadError, setDownloadError] = useState("");
  const [previewRows, setPreviewRows] = useState<PreviewSummary[] | null>(null);
  const [previewID, setPreviewID] = useState<string | null>(null);
  const [backupVerified, setBackupVerified] = useState(false);
  const [manifestID, setManifestID] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const { workspace } = useWorkspace();
  const workspaceId = workspace.id;

  const exportsQuery = useInfinite<ExportRequest>(
    ["portability-exports", workspaceId],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<ExportRequest>>(`/portability/exports?${params.toString()}`, { signal, workspaceId });
    },
  );
  const importsQuery = useInfinite<ImportRequest>(
    ["portability-imports", workspaceId],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<ImportRequest>>(`/portability/imports?${params.toString()}`, { signal, workspaceId });
    },
  );
  const manifestQuery = useQuery<ExportManifest>(
    ["portability-manifest", workspaceId, manifestID],
    (signal) => api.get(`/portability/exports/${encodeURIComponent(manifestID ?? "")}/manifest`, { signal, workspaceId }),
    { enabled: Boolean(manifestID) },
  );

  const startExport = useMutation<void, ExportRequest>(
    () => api.post("/portability/exports", { kind: "workspace" }, { workspaceId, idempotencyKey: idempotencyKey() }),
    { invalidates: [["portability-exports", workspaceId]] },
  );
  const createImport = useMutation<{ file_id: string }, ImportRequest>(
    (input) => api.post("/portability/imports", { ...input, kind: "workspace", auto_start: false }, { workspaceId, idempotencyKey: idempotencyKey() }),
    { invalidates: [["portability-imports", workspaceId]] },
  );
  const confirmImport = useMutation<{ backup_verified: boolean }, ImportRequest>(
    (input) => api.post(`/portability/imports/${encodeURIComponent(previewID ?? "")}/confirm`, input, { workspaceId, idempotencyKey: idempotencyKey() }),
    { invalidates: [["portability-imports", workspaceId]] },
  );
  const uploadAndImport = async (selected: File) => {
    const form = new FormData();
    form.append("file", selected);
    form.append("owner_type", "workspace");
    form.append("owner_id", workspaceId);
    const uploaded = await api.post<{ id: string }>("/files", form, { workspaceId, idempotencyKey: idempotencyKey() });
    await createImport.mutate({ file_id: uploaded.id });
  };
  const downloadExport = async (fileID: string) => {
    setDownloadError("");
    try {
      const response = await fetch(`/api/v1/files/${encodeURIComponent(fileID)}`, { headers: { "Hubchat-Workspace-Id": workspaceId } });
      if (!response.ok) throw new Error("The archive download failed.");
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `hubchat-${workspaceId}.json.gz`;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (error) {
      setDownloadError(error instanceof Error ? error.message : "The archive download failed.");
    }
  };
  const preview = useMutation<string, { data: Array<{ name: string; rows: number }> }>(
    (id) => api.post(`/portability/imports/${encodeURIComponent(id)}/preview`, undefined, { workspaceId }),
  );

  const exportRows = exportsQuery.items;
  const importRows = importsQuery.items;
  const activeExports = exportRows.filter((item) => item.state === "pending" || item.state === "running");
  const uploadError = createImport.error;

  return (
    <Page>
      <PageHeader
        title="Import & export"
        description="Move a complete workspace between Hubchat installations with a versioned, inspectable archive."
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList items={[{ value: "export", label: "Export" }, { value: "import", label: "Import" }, { value: "backups", label: "Backups" }]} />
          </Tabs>
        }
      />

      <PageBody width="narrow">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="export">
            <Callout tone="info" className="mb-4">
              Archives run as durable background jobs. The generated file is stored in the configured file backend, expires after seven days, and remains workspace-scoped.
            </Callout>

            <Section title="In progress">
              <Card>
                <CardBody>
                  {exportsQuery.isLoading ? <p className="text-sm text-fg-muted">Loading export jobs…</p> : exportsQuery.error ? <div className="space-y-2"><p className="text-sm text-danger">Could not load export jobs.</p><Button variant="secondary" size="sm" leading={<RefreshCw />} onClick={exportsQuery.refetch}>Retry</Button></div> : activeExports.length === 0 ? <p className="text-sm text-fg-muted">No export jobs are currently running.</p> : <div className="space-y-2">{activeExports.map((item) => <RequestRow key={item.id} request={item} />)}</div>}
                </CardBody>
              </Card>
            </Section>

            <Section title="Start an export">
              <Card>
                <CardBody className="flex items-center gap-4">
                  <FileArchive className="size-5 shrink-0 text-fg-muted" />
                  <div className="min-w-0 flex-1"><p className="text-sm text-fg">Full workspace archive</p><p className="mt-0.5 text-xs text-fg-muted">Customers, conversations, tickets, settings, integrations, audit records, and attachment metadata in a versioned JSON archive.</p></div>
                  <Button variant="secondary" size="sm" leading={<Download />} loading={startExport.isPending} onClick={() => void startExport.mutate(undefined).catch(() => {})}>Export</Button>
                </CardBody>
              </Card>
              {Boolean(startExport.error) && <p className="mt-2 text-sm text-danger">{errorMessage(startExport.error, "The export could not be started.")}</p>}
            </Section>

            <Section title="Export history">
              <Card>
                <CardBody className="p-0">
                  {exportRows.length === 0 && !exportsQuery.isLoading ? <p className="px-4 py-6 text-sm text-fg-muted">Completed archives will appear here.</p> : <ul className="divide-y divide-line-subtle">{exportRows.map((item) => <li key={item.id} className="flex items-center gap-3 px-4 py-3"><div className="min-w-0 flex-1"><div className="flex items-center gap-2"><span className="truncate font-mono text-xs text-fg">{item.id}</span><Badge tone={statusTone(item.state)}>{statusLabel(item.state)}</Badge></div><p className="mt-1 text-xs text-fg-muted">{item.row_count === undefined ? "Rows pending" : `${item.row_count.toLocaleString()} rows`} · created {displayDate(item.created_at)}{item.expires_at ? ` · expires ${displayDate(item.expires_at)}` : ""}</p>{item.error && <p className="mt-1 text-xs text-danger">{item.error}</p>}</div>{item.file_id && item.state === "completed" && <div className="flex shrink-0 gap-1"><Button variant="ghost" size="sm" onClick={() => setManifestID(item.id)}>Manifest</Button><Button variant="ghost" size="sm" onClick={() => void downloadExport(item.file_id ?? "")}>Download</Button></div>}</li>)}</ul>}
                </CardBody>
              </Card>
              <Pagination hasPrevious={false} hasNext={exportsQuery.hasMore} onPrevious={() => undefined} onNext={() => void exportsQuery.fetchNext()} summary={`${exportRows.length} export${exportRows.length === 1 ? "" : "s"} loaded`} />
              {downloadError && <p className="mt-2 text-sm text-danger">{downloadError}</p>}
              {manifestID && <Card className="mt-3"><CardBody>{manifestQuery.isLoading ? <p className="text-sm text-fg-muted">Loading archive manifest…</p> : manifestQuery.error ? <div className="space-y-2"><p className="text-sm text-danger">Could not load the archive manifest.</p><Button variant="secondary" size="sm" onClick={manifestQuery.refetch}>Retry</Button></div> : manifestQuery.data && <><div className="flex items-start justify-between gap-3"><div><p className="text-sm font-medium text-fg">Archive manifest</p><p className="mt-1 text-xs text-fg-muted">{manifestQuery.data.file_name} · {manifestQuery.data.size_bytes.toLocaleString()} bytes · expires {displayDate(manifestQuery.data.expires_at)}</p></div><Button variant="ghost" size="sm" onClick={() => setManifestID(null)}>Dismiss</Button></div><dl className="mt-3 grid gap-3 text-xs sm:grid-cols-3"><div><dt className="text-fg-muted">Rows</dt><dd className="mt-0.5 tabular text-fg">{manifestQuery.data.row_count.toLocaleString()}</dd></div><div><dt className="text-fg-muted">Attachments</dt><dd className="mt-0.5 tabular text-fg">{manifestQuery.data.attachment_count.toLocaleString()} · {manifestQuery.data.attachment_bytes.toLocaleString()} bytes</dd></div><div><dt className="text-fg-muted">SHA-256</dt><dd className="mt-0.5 break-all font-mono text-2xs text-fg-secondary">{manifestQuery.data.checksum || "Not recorded"}</dd></div></dl><div className="mt-3 max-h-48 overflow-auto rounded-md border border-line"><table className="w-full text-left text-xs"><thead className="border-b border-line bg-inset text-fg-muted"><tr><th className="px-3 py-2 font-medium">Table</th><th className="px-3 py-2 text-right font-medium">Rows</th></tr></thead><tbody className="divide-y divide-line-subtle">{manifestQuery.data.tables.filter((table) => table.rows > 0).map((table) => <tr key={table.name}><td className="px-3 py-2 font-mono text-fg-secondary">{table.name}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{table.rows.toLocaleString()}</td></tr>)}</tbody></table></div></>}</CardBody></Card>}
            </Section>

            <Section title="From the command line">
              <CodeBlock language="bash" code={`hubchat workspace export --slug acme --out ./acme.json.gz\nhubchat workspace import --file ./acme.json.gz --slug acme-restored`} />
            </Section>
          </TabsContent>

          <TabsContent value="import">
            <Callout tone="warning" className="mb-4">Upload a Hubchat workspace archive to preview its row counts before importing. The preview reads the archive without writing tenant records.</Callout>
            <input ref={fileInput} type="file" accept=".gz,.json,application/gzip,application/json" className="sr-only" onChange={(event) => { const selected = event.target.files?.[0]; event.target.value = ""; if (selected) void uploadAndImport(selected).catch(() => {}); }} />
            <Section title="Workspace archive">
              <Card><CardBody className="flex items-center gap-4"><Upload className="size-5 shrink-0 text-fg-muted" /><div className="min-w-0 flex-1"><p className="text-sm text-fg">Import a .json.gz archive</p><p className="mt-0.5 text-xs text-fg-muted">The archive is uploaded as a workspace-owned file and processed by the job queue.</p></div><Button variant="secondary" size="sm" leading={<Upload />} loading={createImport.isPending} onClick={() => fileInput.current?.click()}>Choose file</Button></CardBody></Card>
              {Boolean(uploadError) && <p className="mt-2 text-sm text-danger">{errorMessage(uploadError, "The import could not be started.")}</p>}
            </Section>
            <Section title="Import history">
              <Card><CardBody className="p-0">{importsQuery.isLoading ? <p className="px-4 py-6 text-sm text-fg-muted">Loading import jobs…</p> : importsQuery.error ? <div className="space-y-2 px-4 py-6"><p className="text-sm text-danger">Could not load import jobs.</p><Button variant="secondary" size="sm" leading={<RefreshCw />} onClick={importsQuery.refetch}>Retry</Button></div> : importRows.length === 0 ? <p className="px-4 py-6 text-sm text-fg-muted">Uploaded archives will appear here.</p> : <ul className="divide-y divide-line-subtle">{importRows.map((item) => <li key={item.id} className="flex items-center gap-3 px-4 py-3"><div className="min-w-0 flex-1"><div className="flex items-center gap-2"><span className="truncate font-mono text-xs text-fg">{item.id}</span><Badge tone={statusTone(item.state)}>{statusLabel(item.state)}</Badge></div><p className="mt-1 text-xs text-fg-muted">{item.total_rows === undefined ? "Rows pending" : `${item.total_rows.toLocaleString()} rows`} · {item.processed_rows.toLocaleString()} processed · {item.failed_rows.toLocaleString()} failed · created {displayDate(item.created_at)}</p></div>{item.state === "pending" && <Button variant="ghost" size="sm" loading={preview.isPending} onClick={() => { setPreviewID(item.id); setBackupVerified(false); void preview.mutate(item.id).then((result) => setPreviewRows(result.data)).catch(() => {}); }}>Preview</Button>}</li>)}</ul>}</CardBody></Card>
              <Pagination hasPrevious={false} hasNext={importsQuery.hasMore} onPrevious={() => undefined} onNext={() => void importsQuery.fetchNext()} summary={`${importRows.length} import${importRows.length === 1 ? "" : "s"} loaded`} />
              {Boolean(preview.error) && <p className="mt-2 text-sm text-danger">{errorMessage(preview.error, "The archive preview failed.")}</p>}
              {previewRows && <Card className="mt-3"><CardBody><div className="flex items-center justify-between gap-3"><div><p className="text-sm font-medium text-fg">Preview result</p><p className="mt-1 text-xs text-fg-muted">Existing rows will be skipped by the idempotent importer. New rows are candidates for insertion.</p></div><Button variant="ghost" size="sm" onClick={() => { setPreviewRows(null); setPreviewID(null); }}>Dismiss</Button></div><div className="mt-3 max-h-64 overflow-auto rounded-md border border-line"><table className="w-full text-left text-xs"><thead className="border-b border-line bg-inset text-fg-muted"><tr><th className="px-3 py-2 font-medium">Table</th><th className="px-3 py-2 text-right font-medium">Rows</th><th className="px-3 py-2 text-right font-medium">Existing</th><th className="px-3 py-2 text-right font-medium">New</th></tr></thead><tbody className="divide-y divide-line-subtle">{previewRows.filter((summary) => summary.rows > 0).map((summary) => <tr key={summary.name}><td className="px-3 py-2 font-mono text-fg-secondary">{summary.name}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{summary.rows.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-warning-text">{(summary.existing ?? 0).toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-success-text">{(summary.new ?? summary.rows).toLocaleString()}</td></tr>)}</tbody></table></div><label className="mt-4 flex items-start gap-2 text-sm text-fg"><input type="checkbox" checked={backupVerified} onChange={(event) => setBackupVerified(event.target.checked)} className="mt-0.5" /> <span>I verified a current PostgreSQL and file-storage backup and reviewed the conflict preview.</span></label><div className="mt-3 flex justify-end"><Button variant="primary" loading={confirmImport.isPending} disabled={!backupVerified || !previewID} onClick={() => void confirmImport.mutate({ backup_verified: true }).then(() => { setPreviewRows(null); setPreviewID(null); setBackupVerified(false); }).catch(() => {})}>Confirm and import</Button></div>{Boolean(confirmImport.error) && <p className="mt-2 text-sm text-danger">{errorMessage(confirmImport.error, "The import could not be confirmed.")}</p>}</CardBody></Card>}
            </Section>
          </TabsContent>

          <TabsContent value="backups">
            <Callout tone="info" className="mb-4">Hubchat does not take database backups for you on a self-hosted deployment. PostgreSQL and file storage remain the source of truth; logical archives provide a portable restore path.</Callout>
            <Section title="What to back up"><Card><CardBody><ul className="space-y-3 text-sm"><li><p className="text-fg">PostgreSQL database</p><p className="mt-0.5 text-xs text-fg-muted">Everything except attachment bytes lives here.</p></li><li><p className="text-fg">File storage</p><p className="mt-0.5 text-xs text-fg-muted">The local data directory or configured S3-compatible bucket.</p></li><li><p className="text-fg">Secret key</p><p className="mt-0.5 text-xs text-fg-muted">Without <code className="font-mono">HUBCHAT_SECRET_KEY</code>, encrypted integration secrets cannot be decrypted after restore.</p></li></ul></CardBody></Card></Section>
            <Section title="Restore procedure"><CodeBlock language="bash" code={`# 1. Restore the database\npg_restore --clean --if-exists -d hubchat hubchat-2026-07-26.dump\n\n# 2. Restore files\nrsync -a backup/files/ /var/lib/hubchat/files/\n\n# 3. Verify before serving traffic\nhubchat migrate status\nhubchat doctor --json`} /></Section>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}

function RequestRow({ request }: { request: ExportRequest }) {
  return <div className="flex items-center gap-2"><Badge tone={statusTone(request.state)}>{statusLabel(request.state)}</Badge><span className="font-mono text-xs text-fg-muted">{request.id}</span></div>;
}
