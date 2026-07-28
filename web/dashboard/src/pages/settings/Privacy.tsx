import {
  api,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  Input,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  Select,
  SettingsRow,
  Switch,
  Textarea,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { useState } from "react";

const RETENTION_CATEGORIES = [
  { key: "conversations", label: "Conversations and messages", detail: "Including attachments referenced by them." },
  { key: "events", label: "Customer events", detail: "High-volume. The most useful thing to expire aggressively." },
  { key: "sessions", label: "Contact sessions and page views", detail: "Presence history, not the conversation itself." },
  { key: "audit", label: "Audit log", detail: "Append-only. Shortening this weakens your own incident response." },
  { key: "webhooks", label: "Webhook delivery history", detail: "Replay is only possible inside this window." },
  { key: "surveys", label: "Survey responses", detail: "Aggregates survive deletion of the individual response." },
];

type PrivacySettings = {
  ip_storage: "full" | "country_only" | "none";
  track_page_views: boolean;
  retention_days: Record<string, number>;
  allowed_local_storage_keys: string[];
  allowed_cookie_keys: string[];
  require_consent: boolean;
  consent_notice: string;
  privacy_policy_url: string;
};

type Settings = { privacy: PrivacySettings };

/** Privacy and retention (§12). */
export default function Privacy() {
  const settings = useQuery<Settings>(["workspace-settings"], (signal) =>
    api.get<Settings>("/workspace/settings", { signal }),
  );

  return (
    <Page>
      <PageHeader
        title="Privacy & retention"
        description="What Hubchat keeps, for how long, and what data collection is enabled."
      />

      <PageBody width="narrow">
        <QueryBoundary query={settings}>{(data) => <PrivacyForm initial={data.privacy} />}</QueryBoundary>
      </PageBody>
    </Page>
  );
}

const RETENTION_OPTIONS = [
  { value: "0", label: "Keep indefinitely" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "730", label: "2 years" },
];

function PrivacyForm({ initial }: { initial: PrivacySettings }) {
  const [ipStorage, setIpStorage] = useState(initial.ip_storage);
  const [trackPageViews, setTrackPageViews] = useState(initial.track_page_views);
  const [retention, setRetention] = useState<Record<string, number>>(initial.retention_days ?? {});
  const [localStorageKeys, setLocalStorageKeys] = useState(initial.allowed_local_storage_keys.join(", "));
  const [cookieKeys, setCookieKeys] = useState(initial.allowed_cookie_keys.join(", "));
  const [requireConsent, setRequireConsent] = useState(initial.require_consent);
  const [consentNotice, setConsentNotice] = useState(initial.consent_notice);
  const [policyUrl, setPolicyUrl] = useState(initial.privacy_policy_url);

  const save = useMutation<PrivacySettings, unknown>(
    (body) => api.patch("/workspace/privacy", body),
    { invalidates: [["workspace-settings"]] },
  );

  const parsedLocalStorageKeys = localStorageKeys.split(",").map((v) => v.trim()).filter(Boolean);
  const parsedCookieKeys = cookieKeys.split(",").map((v) => v.trim()).filter(Boolean);

  const dirty =
    ipStorage !== initial.ip_storage ||
    trackPageViews !== initial.track_page_views ||
    JSON.stringify(retention) !== JSON.stringify(initial.retention_days ?? {}) ||
    parsedLocalStorageKeys.join(",") !== initial.allowed_local_storage_keys.join(",") ||
    parsedCookieKeys.join(",") !== initial.allowed_cookie_keys.join(",") ||
    requireConsent !== initial.require_consent ||
    consentNotice !== initial.consent_notice ||
    policyUrl !== initial.privacy_policy_url;

  return (
    <>
      <div className="mb-4 flex justify-end">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty}
          loading={save.isPending}
          onClick={() =>
            void save.mutate({
              ip_storage: ipStorage,
              track_page_views: trackPageViews,
              retention_days: retention,
              allowed_local_storage_keys: parsedLocalStorageKeys,
              allowed_cookie_keys: parsedCookieKeys,
              require_consent: requireConsent,
              consent_notice: consentNotice,
              privacy_policy_url: policyUrl,
            })
          }
        >
          Save changes
        </Button>
      </div>

      {save.error ? (
        <Callout tone="danger" className="mb-4">
          {save.error instanceof ApiError ? save.error.message : "Could not save these settings."}
        </Callout>
      ) : null}
      {save.isSuccess ? (
        <Callout tone="success" className="mb-4">
          Saved.
        </Callout>
      ) : null}

      <Section title="Retention" description="Records past their window are deleted or anonymised by a scheduled job.">
        <Card>
          <CardBody className="pt-0">
            {RETENTION_CATEGORIES.map((category) => (
              <SettingsRow key={category.key} label={category.label} description={category.detail}>
                <Select
                  size="sm"
                  value={String(retention[category.key] ?? 0)}
                  onValueChange={(value) =>
                    setRetention((current) => ({ ...current, [category.key]: Number(value) }))
                  }
                  aria-label={`${category.label} retention`}
                  options={RETENTION_OPTIONS}
                />
              </SettingsRow>
            ))}
          </CardBody>
        </Card>
      </Section>

      <Section title="Collection">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow
              label="Store IP addresses"
              description="Country-only keeps enough for abuse handling without identifying a household."
            >
              <Select
                size="sm"
                value={ipStorage}
                onValueChange={(value) => setIpStorage(value as PrivacySettings["ip_storage"])}
                aria-label="IP storage"
                options={[
                  { value: "full", label: "Store in full" },
                  { value: "country_only", label: "Country only" },
                  { value: "none", label: "Do not store" },
                ]}
              />
            </SettingsRow>

            <SettingsRow
              label="Track page views"
              description="Records which pages a visitor saw before contacting you. Off means the agent sees only the current URL."
            >
              <Switch checked={trackPageViews} onCheckedChange={setTrackPageViews} aria-label="Track page views" />
            </SettingsRow>

            <SettingsRow
              label="Allowed local-storage keys"
              description="Comma-separated. Only these keys may be read from the host page and attached to a session."
            >
              <Input
                inputSize="sm"
                mono
                value={localStorageKeys}
                onChange={(event) => setLocalStorageKeys(event.target.value)}
                placeholder="app_version, experiment_bucket"
              />
            </SettingsRow>

            <SettingsRow label="Allowed cookie keys" description="Comma-separated. Empty means none are read.">
              <Input
                inputSize="sm"
                mono
                value={cookieKeys}
                onChange={(event) => setCookieKeys(event.target.value)}
                placeholder="locale"
              />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>

      <Section title="Consent">
        <Card>
          <CardBody className="pt-0">
            <SettingsRow
              label="Require consent before the first message"
              description="The visitor must acknowledge the notice before a conversation can start."
            >
              <Switch checked={requireConsent} onCheckedChange={setRequireConsent} aria-label="Require consent" />
            </SettingsRow>

            <SettingsRow label="Consent notice">
              <Textarea rows={3} value={consentNotice} onChange={(event) => setConsentNotice(event.target.value)} />
            </SettingsRow>

            <SettingsRow label="Privacy policy URL" description="Linked from the widget, the portal footer, and outbound email.">
              <Input inputSize="sm" mono value={policyUrl} onChange={(event) => setPolicyUrl(event.target.value)} />
            </SettingsRow>
          </CardBody>
        </Card>
      </Section>
    </>
  );
}
