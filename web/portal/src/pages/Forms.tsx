import {
  ApiError,
  Breadcrumbs,
  Button,
  Card,
  CardBody,
  EmptyState,
  Field,
  Input,
  Select,
  Textarea,
  api,
  formConditionApplies,
  idempotencyKey,
  useQuery,
} from "@hubchat/shared";
import { ArrowRight, CheckCircle2, ClipboardList, Lock, UploadCloud } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { portalErrorMessage, usePortal } from "../portal-context";
import { portalText } from "../i18n";

type FormField = {
  key: string;
  label: string;
  type: string;
  placeholder?: string;
  description?: string;
  options?: string[];
  required: boolean;
  default_value?: unknown;
  condition?: Record<string, unknown>;
};

type PortalForm = {
  id: string;
  name: string;
  slug: string;
  description?: string;
  purpose: string;
  confirmation?: Record<string, unknown>;
  access: "public" | "authenticated";
  fields: FormField[];
};

type FormPage = { data: PortalForm[]; next_cursor: string | null; has_more: boolean };
type UploadedFile = { id: string; name: string };
type SubmissionResult = { id: string; status: string; confirmation?: Record<string, unknown> };

function signInPath(path: string, portalID: string) {
  return `/sign-in?portal=${encodeURIComponent(portalID)}&next=${encodeURIComponent(path)}`;
}

function fieldInputType(type: string) {
  if (type === "email") return "email";
  if (type === "integer" || type === "decimal") return "number";
  if (type === "url") return "url";
  return "text";
}

function confirmationText(value: Record<string, unknown> | undefined) {
  const message = value?.message ?? value?.text;
  return typeof message === "string" && message.trim() ? message : "Thanks — your response has been received.";
}

export default function Forms() {
  const { slug } = useParams();
  const location = useLocation();
  const { data: portalData } = usePortal();
  const workspaceID = portalData?.portal.workspace_id ?? "";
  const portalID = portalData?.portal.id ?? "";
  const list = useQuery<FormPage>(
    ["portal-forms", workspaceID],
    (signal) => api.get(`/portal/forms${portalData?.portal.id ? `?portal=${encodeURIComponent(portalData.portal.id)}` : ""}`, { signal }),
    { enabled: Boolean(workspaceID) },
  );
  const selected = list.data?.data.find((item) => item.slug === slug);
  const detail = useQuery<PortalForm>(
    ["portal-form", portalID, slug ?? ""],
    (signal) => api.get(`/portal/forms/${encodeURIComponent(slug ?? "")}?portal=${encodeURIComponent(portalID)}`, { signal }),
    { enabled: Boolean(slug && portalID) },
  );

  if (!slug) return <FormDirectory query={list} />;
  return <FormDetail form={detail.data ?? selected} query={detail} portalID={portalID} locationPath={`${location.pathname}${location.search}`} />;
}

function FormDirectory({ query }: { query: ReturnType<typeof useQuery<FormPage>> }) {
  const { data: portal } = usePortal();
  const t = (key: string, fallback: string) => portalText(portal, key, fallback);
  if (query.isLoading) return <div className="py-14 text-center text-sm text-fg-muted">{t("loading_forms", "Loading forms…")}</div>;
  if (query.isError) return <EmptyState icon={ClipboardList} variant="error" title={t("forms_unavailable", "Forms unavailable")} description={portalErrorMessage(query.error)} action={<Button size="sm" variant="secondary" onClick={() => query.refetch()}>{t("try_again", "Try again")}</Button>} />;
  const forms = query.data?.data ?? [];
  return <div className="mx-auto max-w-3xl">
    <Breadcrumbs className="mb-4" items={[{ label: t("forms", "Forms") }]} />
    <header className="mb-6"><h1 className="text-2xl font-semibold tracking-tight text-fg">{t("contact_forms", "Contact forms")}</h1><p className="mt-1.5 text-sm text-fg-muted">{t("choose_form", "Choose the form that best matches what you need.")}</p></header>
    {forms.length === 0 ? <EmptyState icon={ClipboardList} title={t("no_forms", "No forms available")} description={t("no_forms_description", "There are no published forms here yet.")} /> : <div className="grid gap-3 sm:grid-cols-2">{forms.map((form) => <Card key={form.id} interactive className="p-0"><Link to={`/forms/${encodeURIComponent(form.slug)}`} className="block p-4"><div className="flex items-start gap-3"><div className="grid size-9 shrink-0 place-items-center rounded-lg border border-accent-border bg-accent-subtle"><ClipboardList aria-hidden="true" className="size-4 text-accent-text" /></div><div className="min-w-0 flex-1"><div className="flex items-center gap-1.5"><h2 className="truncate text-sm font-medium text-fg">{form.name}</h2>{form.access === "authenticated" && <Lock aria-label={t("sign_in_required", "Sign-in required")} className="size-3 shrink-0 text-fg-muted" />}</div>{form.description && <p className="mt-1 line-clamp-3 text-xs leading-normal text-fg-muted">{form.description}</p>}</div><ArrowRight aria-hidden="true" className="mt-1 size-3.5 shrink-0 text-fg-disabled" /></div></Link></Card>)}</div>}
  </div>;
}

function FormDetail({ form, query, portalID, locationPath }: { form?: PortalForm; query: ReturnType<typeof useQuery<PortalForm>>; portalID: string; locationPath: string }) {
  const { data: portal } = usePortal();
  const t = (key: string, fallback: string) => portalText(portal, key, fallback);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [fileIDs, setFileIDs] = useState<Record<string, string>>({});
  const [uploading, setUploading] = useState<string | null>(null);
  const [uploadError, setUploadError] = useState("");
  const [submitError, setSubmitError] = useState("");
  const [submitted, setSubmitted] = useState<SubmissionResult | null>(null);

  useEffect(() => {
    if (!form) return;
    const defaults: Record<string, unknown> = {};
    for (const field of form.fields) if (field.default_value !== undefined) defaults[field.key] = field.default_value;
    setValues(defaults);
    setFileIDs({});
    setSubmitted(null);
    setSubmitError("");
  }, [form]);

  if (query.isLoading && !form) return <div className="py-14 text-center text-sm text-fg-muted">{t("loading_form", "Loading form…")}</div>;
  if (query.isError && !form) {
    const unauthorized = query.error instanceof ApiError && query.error.isUnauthorized;
    return <EmptyState icon={unauthorized ? Lock : ClipboardList} variant={unauthorized ? "empty" : "error"} title={unauthorized ? t("sign_in_use_form", "Sign in to use this form") : t("form_unavailable", "Form unavailable")} description={unauthorized ? t("form_authenticated", "This form is available to signed-in customers.") : portalErrorMessage(query.error)} action={unauthorized ? <Button size="sm" variant="primary" asChild><Link to={signInPath(locationPath, portalID)}>{t("sign_in", "Sign in")}</Link></Button> : <Button size="sm" variant="secondary" onClick={() => query.refetch()}>{t("try_again", "Try again")}</Button>} />;
  }
  if (!form) return <EmptyState icon={ClipboardList} title={t("form_not_found", "Form not found")} description={t("form_unpublished", "This form may have been unpublished.")} action={<Button size="sm" variant="secondary" asChild><Link to="/forms">{t("back_to_forms", "Back to forms")}</Link></Button>} />;

  const visible = (field: FormField) => formConditionApplies(field.condition, values);
  const setValue = (key: string, value: unknown) => setValues((current) => ({ ...current, [key]: value }));
  const upload = async (field: FormField, file: File | undefined) => {
    if (!file) return;
    setUploading(field.key);
    setUploadError("");
    try {
      const body = new FormData();
      body.append("file", file);
      const uploaded = await api.post<UploadedFile>(`/portal/forms/${encodeURIComponent(form.slug)}/files?portal=${encodeURIComponent(portalID)}`, body, { idempotencyKey: idempotencyKey() });
      setFileIDs((current) => ({ ...current, [field.key]: uploaded.id }));
    } catch (error) {
      setUploadError(error instanceof ApiError ? error.message : "The attachment could not be uploaded.");
    } finally {
      setUploading(null);
    }
  };
  const submit = async () => {
    setSubmitError("");
    const visibleKeys = new Set(form.fields.filter(visible).map((field) => field.key));
    const submittedValues = Object.fromEntries(Object.entries(values).filter(([key]) => visibleKeys.has(key) && !form.fields.find((field) => field.key === key && field.type === "file")));
    const submittedFiles = Object.fromEntries(Object.entries(fileIDs).filter(([key]) => visibleKeys.has(key)));
    try {
      const result = await api.post<SubmissionResult>(`/portal/forms/${encodeURIComponent(form.slug)}/submissions?portal=${encodeURIComponent(portalID)}`, { values: submittedValues, file_ids: submittedFiles, source_url: window.location.href }, { idempotencyKey: idempotencyKey() });
      setSubmitted(result);
    } catch (error) {
      setSubmitError(error instanceof ApiError ? error.message : "The form could not be submitted. Your answers are still here.");
    }
  };

  if (submitted) return <div className="mx-auto max-w-xl"><Breadcrumbs className="mb-4" items={[{ label: t("forms", "Forms"), href: "/forms" }, { label: form.name }]} /><Card><CardBody className="py-12 text-center"><div className="mx-auto mb-4 grid size-11 place-items-center rounded-xl border border-success-border bg-success-subtle"><CheckCircle2 aria-hidden="true" className="size-5 text-success-text" /></div><h1 className="text-xl font-semibold tracking-tight text-fg">{t("response_received", "Response received")}</h1><p className="mx-auto mt-2 max-w-md text-sm leading-normal text-fg-muted">{confirmationText(submitted.confirmation ?? form.confirmation)}</p><Button className="mt-6" size="sm" variant="secondary" asChild><Link to="/forms">{t("back_to_forms", "Back to forms")}</Link></Button></CardBody></Card></div>;

  return <div className="mx-auto max-w-xl"><Breadcrumbs className="mb-4" items={[{ label: t("forms", "Forms"), href: "/forms" }, { label: form.name }]} /><header className="mb-6"><h1 className="text-2xl font-semibold tracking-tight text-fg">{form.name}</h1>{form.description && <p className="mt-1.5 text-sm leading-normal text-fg-muted">{form.description}</p>}</header><Card><CardBody className="space-y-5"><div className="space-y-4">{form.fields.filter(visible).map((field) => <Field key={field.key} label={field.label} description={field.description} required={field.required}>
    {field.type === "file" ? <label className="flex cursor-pointer flex-col items-center gap-1.5 rounded-lg border border-dashed border-line px-4 py-7 text-center transition-colors hover:border-line-strong hover:bg-fill"><UploadCloud aria-hidden="true" className="size-5 text-fg-muted" /><span className="text-xs text-fg-secondary">{fileIDs[field.key] ? t("file_ready", "File ready") : t("choose_file", "Choose a file")}</span><input type="file" required={field.required && !fileIDs[field.key]} className="sr-only" onChange={(event) => void upload(field, event.target.files?.[0])} />{uploading === field.key && <span className="text-2xs text-fg-muted">{t("uploading", "Uploading…")}</span>}</label> : field.type === "text" ? <Textarea rows={5} required={field.required} value={String(values[field.key] ?? "")} placeholder={field.placeholder} onChange={(event) => setValue(field.key, event.target.value)} /> : field.type === "enum" ? <Select value={String(values[field.key] ?? "")} onValueChange={(value) => setValue(field.key, value)} options={(field.options ?? []).map((option) => ({ value: option, label: option }))} placeholder={t("choose_one", "Choose…")} aria-label={field.label} /> : field.type === "boolean" ? <label className="flex items-center gap-2 text-sm text-fg"><input type="checkbox" checked={Boolean(values[field.key])} onChange={(event) => setValue(field.key, event.target.checked)} className="size-4 accent-accent" />{t("yes", "Yes")}</label> : <Input type={fieldInputType(field.type)} required={field.required} value={String(values[field.key] ?? "")} placeholder={field.placeholder} onChange={(event) => setValue(field.key, field.type === "integer" || field.type === "decimal" ? (event.target.value === "" ? "" : Number(event.target.value)) : event.target.value)} />}
  </Field>)}</div>{uploadError && <p role="alert" className="text-sm text-danger-text">{uploadError}</p>}{submitError && <p role="alert" className="text-sm text-danger-text">{submitError}</p>}<div className="flex items-center justify-between gap-3 border-t border-line pt-4"><Button variant="ghost" size="sm" asChild><Link to="/forms">{t("cancel", "Cancel")}</Link></Button><Button variant="primary" size="md" disabled={uploading !== null} onClick={() => void submit()}>{t("send_response", "Send response")}</Button></div></CardBody></Card></div>;
}
