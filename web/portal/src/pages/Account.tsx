import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  ConfirmDialog,
  EmptyState,
  Input,
  Section,
  Select,
  SettingsRow,
  Switch,
  api,
  useMutation,
} from "@hubchat/shared";
import { Download, Trash2, UserRound } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { usePortal, type PortalNotificationPreferences, type PortalViewer } from "../portal-context";
import { Link } from "react-router-dom";
import { portalText } from "../i18n";

type ProfileResponse = { viewer: PortalViewer; preferences: PortalNotificationPreferences };
type ProfileUpdate = { name?: string; language?: string; timezone?: string };

const LANGUAGE_OPTIONS = [
  { value: "en", label: "English" },
  { value: "fr", label: "Français" },
  { value: "es", label: "Español" },
  { value: "de", label: "Deutsch" },
  { value: "pt", label: "Português" },
  { value: "sw", label: "Kiswahili" },
  { value: "ar", label: "العربية" },
];

const TIMEZONE_OPTIONS = [
  { value: "UTC", label: "UTC" },
  { value: "Africa/Kigali", label: "Kigali (CAT)" },
  { value: "Europe/London", label: "London (GMT/BST)" },
  { value: "America/New_York", label: "New York (ET)" },
  { value: "America/Los_Angeles", label: "Los Angeles (PT)" },
  { value: "Asia/Dubai", label: "Dubai (GST)" },
  { value: "Asia/Kolkata", label: "India (IST)" },
];

/** Customer profile and communication preferences (§6.5). */
export default function Account() {
  const { data, refetch } = usePortal();
  const t = (key: string, fallback: string) => portalText(data, key, fallback);
  const viewer = data?.viewer;
  const [name, setName] = useState("");
  const [language, setLanguage] = useState("en");
  const [timezone, setTimezone] = useState("UTC");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState("");
  const profile = useMutation<ProfileUpdate, ProfileResponse>((input) => api.patch("/portal/me", input), { onSuccess: () => refetch() });
  const preference = useMutation<{ key: keyof PortalNotificationPreferences; value: boolean }, ProfileResponse>(({ key, value }) => api.patch("/portal/me", { preferences: { [key]: value } }), { onSuccess: () => refetch() });
  const deletion = useMutation<{ confirmation: string }, void>(({ confirmation }) => api.post("/portal/me/delete", { confirmation }), { onSuccess: () => { window.location.assign("/portal/sign-in?deleted=1"); } });
  useEffect(() => {
    if (!viewer) return;
    setName(viewer.name);
    setLanguage(viewer.language || data?.portal.default_language || "en");
    setTimezone(viewer.timezone || "UTC");
  }, [data?.portal.default_language, viewer]);
  if (!viewer) return <EmptyState icon={UserRound} title={t("sign_in_profile", "Sign in to view your profile")} description={t("profile_after_signin", "Your portal profile is available after email sign-in.")} action={<Button variant="primary" size="sm" asChild><Link to="/sign-in">{t("sign_in", "Sign in")}</Link></Button>} />;
  const preferences = data.preferences ?? { ticket_status: true, feedback_updates: true, changelog: false, surveys: true };
  const saveProfile = (event: FormEvent) => {
    event.preventDefault();
    const value = name.trim();
    if (value && value !== viewer.name) void profile.mutate({ name: value }).catch(() => {});
  };
  const saveLanguage = (value: string) => {
    const previous = language;
    setLanguage(value);
    void profile.mutate({ language: value }).catch(() => setLanguage(previous));
  };
  const saveTimezone = (value: string) => {
    const previous = timezone;
    setTimezone(value);
    void profile.mutate({ timezone: value }).catch(() => setTimezone(previous));
  };
  const downloadData = async () => {
    setExporting(true);
    setExportError("");
    try {
      const response = await fetch("/api/v1/portal/me/export", { credentials: "same-origin" });
      if (!response.ok) throw new Error("Could not prepare your data export.");
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = "hubchat-customer-export.json";
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) {
      setExportError(error instanceof Error ? error.message : "Could not download your data export.");
    } finally {
      setExporting(false);
    }
  };
  return (
    <div className="mx-auto max-w-2xl">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tighter text-fg">{t("your_profile", "Your profile")}</h1>
        <p className="mt-1.5 text-sm text-fg-muted">
          {t("profile_description", "How we reach you, and what we send.")}
        </p>
      </header>

      <Section title={t("details", "Details")}>
        <Card>
          <CardBody className="pt-0"><form onSubmit={saveProfile}>
            <SettingsRow label={t("photo", "Photo")}>
              <div className="flex items-center gap-3">
                <Avatar name={viewer.name} seed={viewer.email} size="lg" />
                <span className="text-xs text-fg-muted">{t("profile_photos_managed", "Profile photos are managed by your support team.")}</span>
              </div>
            </SettingsRow>

            <SettingsRow label={t("name", "Name")} htmlFor="name">
              <Input id="name" inputSize="sm" value={name} onChange={(event) => setName(event.target.value)} />
            </SettingsRow>

            <SettingsRow
              label={t("email", "Email")}
              description={t("email_description", "Used to sign in and to deliver replies. Changing it requires verification.")}
              htmlFor="email"
            >
              <div className="flex items-center gap-2">
                <Input id="email" inputSize="sm" defaultValue={viewer.email} readOnly />
                <Badge tone="success">{t("verified", "Verified")}</Badge>
              </div>
            </SettingsRow>

            <SettingsRow label={t("company", "Company")}>
              <span className="text-sm text-fg-secondary">{viewer.company}</span>
            </SettingsRow>
            <div className="mt-4 flex items-center justify-end gap-3 border-t border-line pt-4"><Button type="submit" variant="primary" size="sm" loading={profile.isPending} disabled={!name.trim() || name.trim() === viewer.name}>{t("save_profile", "Save profile")}</Button></div>{Boolean(profile.error) && <p className="mt-2 text-right text-xs text-danger">{t("profile_save_error", "Could not save your profile. Please try again.")}</p>}
          </form></CardBody>
        </Card>
      </Section>

      <Section title={t("notifications", "Notifications")}>
        <Card>
          <CardBody className="space-y-3">
            <Switch
              label={t("replies_requests", "Replies to my requests")}
              description={t("replies_description", "You will always receive these — they are how a conversation works.")}
              defaultChecked
              disabled
            />
            <Switch
              label={t("status_changes", "Status changes on my requests")}
              description={t("status_changes_description", "When something moves to resolved or is put on hold.")}
              checked={preferences.ticket_status}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "ticket_status", value }).catch(() => {})}
            />
            <Switch
              label={t("feedback_updates", "Updates on feedback I follow")}
              description={t("feedback_updates_description", "Only when the status changes, not on every comment.")}
              checked={preferences.feedback_updates}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "feedback_updates", value }).catch(() => {})}
            />
            <Switch
              label={t("product_changelog", "Product changelog")}
              description={t("changelog_description_short", "A short email when we ship something notable. Roughly monthly.")}
              checked={preferences.changelog}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "changelog", value }).catch(() => {})}
            />
            <Switch
              label={t("satisfaction_surveys", "Satisfaction surveys")}
              description={t("survey_description", "One short question after a request is resolved.")}
              checked={preferences.surveys}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "surveys", value }).catch(() => {})}
            />
            {Boolean(preference.error) && <p className="text-xs text-danger">{t("preferences_error", "Could not update notification preferences. Please try again.")}</p>}
          </CardBody>
        </Card>
      </Section>

      <Section title={t("language_time", "Language and time")}>
        <Card>
          <CardBody>
            <div className="grid gap-4 sm:grid-cols-2">
              <SettingsRow label={t("language", "Language")} description={t("language_description", "Used for help content when a translation is available.")}>
                <Select value={language} onValueChange={saveLanguage} options={LANGUAGE_OPTIONS} aria-label={t("language", "Language")} disabled={profile.isPending} />
              </SettingsRow>
              <SettingsRow label={t("time_zone", "Time zone")} description={t("timezone_description", "Used when displaying request dates and times.")}>
                <Select value={timezone} onValueChange={saveTimezone} options={TIMEZONE_OPTIONS} aria-label={t("time_zone", "Time zone")} disabled={profile.isPending} />
              </SettingsRow>
            </div>
            {Boolean(profile.error) && <p className="mt-3 text-xs text-danger">{t("language_save_error", "Could not save your language or time zone. Please try again.")}</p>}
          </CardBody>
        </Card>
      </Section>

      <Section title={t("your_data", "Your data")}>
        <Callout tone="info" className="mb-3">
          {t("data_description", "You can take a copy of everything associated with your account, or ask us to delete it.")}
          Deletion removes your messages and profile; anonymised aggregates we are required to keep
          for reporting cannot identify you.
        </Callout>

        <Card>
          <CardHeader
            title={t("download_data", "Download your data")}
            description={t("download_description", "A machine-readable archive of your requests, messages, and profile.")}
            actions={
              <Button variant="secondary" size="sm" leading={<Download />} loading={exporting} onClick={() => void downloadData()}>
                {t("download_export", "Download export")}
              </Button>
            }
          />
        </Card>
        {exportError && <p className="mt-2 text-sm text-danger">{exportError}</p>}

        <Card className="mt-3 border-danger-border">
          <CardHeader
            title={t("delete_account", "Delete your account")}
            description={t("delete_description", "Anonymises your profile and customer context while retaining anonymised support history. This cannot be undone.")}
            actions={
              <Button variant="danger" size="sm" leading={<Trash2 />} onClick={() => setDeleteOpen(true)}>
                {t("delete_account", "Delete account")}
              </Button>
            }
          />
        </Card>
      </Section>
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("delete_anonymise", "Delete and anonymise your account")}
        description={t("delete_confirm_description", "Your name, email, profile attributes, sessions, and customer activity context will be erased. Existing support history will remain anonymised so the support team can retain its records.")}
        confirmLabel={t("delete_account", "Delete account")}
        destructive
        loading={deletion.isPending}
        confirmationPhrase="DELETE"
        onConfirm={() => void deletion.mutate({ confirmation: "DELETE" }).catch(() => {})}
      />
      {Boolean(deletion.error) && <p className="mt-3 text-sm text-danger">{t("delete_error", "Could not delete your account. Please try again.")}</p>}
    </div>
  );
}
