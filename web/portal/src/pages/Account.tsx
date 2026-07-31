import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  EmptyState,
  Input,
  Section,
  SettingsRow,
  Switch,
  api,
  useMutation,
} from "@hubchat/shared";
import { Download, Trash2, UserRound } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { usePortal, type PortalNotificationPreferences, type PortalViewer } from "../portal-context";
import { Link } from "react-router-dom";

type ProfileResponse = { viewer: PortalViewer; preferences: PortalNotificationPreferences };

/** Customer profile and communication preferences (§6.5). */
export default function Account() {
  const { data, refetch } = usePortal();
  const viewer = data?.viewer;
  const [name, setName] = useState("");
  const profile = useMutation<{ name: string }, ProfileResponse>(({ name: value }) => api.patch("/portal/me", { name: value }), { onSuccess: () => refetch() });
  const preference = useMutation<{ key: keyof PortalNotificationPreferences; value: boolean }, ProfileResponse>(({ key, value }) => api.patch("/portal/me", { preferences: { [key]: value } }), { onSuccess: () => refetch() });
  useEffect(() => {
    if (viewer) setName(viewer.name);
  }, [viewer]);
  if (!viewer) return <EmptyState icon={UserRound} title="Sign in to view your profile" description="Your portal profile is available after email sign-in." action={<Button variant="primary" size="sm" asChild><Link to="/sign-in">Sign in</Link></Button>} />;
  const preferences = data.preferences ?? { ticket_status: true, feedback_updates: true, changelog: false, surveys: true };
  const saveProfile = (event: FormEvent) => {
    event.preventDefault();
    const value = name.trim();
    if (value && value !== viewer.name) void profile.mutate({ name: value }).catch(() => {});
  };
  return (
    <div className="mx-auto max-w-2xl">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tighter text-fg">Your profile</h1>
        <p className="mt-1.5 text-sm text-fg-muted">
          How we reach you, and what we send.
        </p>
      </header>

      <Section title="Details">
        <Card>
          <CardBody className="pt-0"><form onSubmit={saveProfile}>
            <SettingsRow label="Photo">
              <div className="flex items-center gap-3">
                <Avatar name={viewer.name} seed={viewer.email} size="lg" />
                <Button variant="secondary" size="sm">
                  Upload
                </Button>
              </div>
            </SettingsRow>

            <SettingsRow label="Name" htmlFor="name">
              <Input id="name" inputSize="sm" value={name} onChange={(event) => setName(event.target.value)} />
            </SettingsRow>

            <SettingsRow
              label="Email"
              description="Used to sign in and to deliver replies. Changing it requires verification."
              htmlFor="email"
            >
              <div className="flex items-center gap-2">
                <Input id="email" inputSize="sm" defaultValue={viewer.email} />
                <Badge tone="success">Verified</Badge>
              </div>
            </SettingsRow>

            <SettingsRow label="Company">
              <span className="text-sm text-fg-secondary">{viewer.company}</span>
            </SettingsRow>
            <div className="mt-4 flex items-center justify-end gap-3 border-t border-line pt-4"><Button type="submit" variant="primary" size="sm" loading={profile.isPending} disabled={!name.trim() || name.trim() === viewer.name}>Save profile</Button></div>{Boolean(profile.error) && <p className="mt-2 text-right text-xs text-danger">Could not save your profile. Please try again.</p>}
          </form></CardBody>
        </Card>
      </Section>

      <Section title="Notifications">
        <Card>
          <CardBody className="space-y-3">
            <Switch
              label="Replies to my requests"
              description="You will always receive these — they are how a conversation works."
              defaultChecked
              disabled
            />
            <Switch
              label="Status changes on my requests"
              description="When something moves to resolved or is put on hold."
              checked={preferences.ticket_status}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "ticket_status", value }).catch(() => {})}
            />
            <Switch
              label="Updates on feedback I follow"
              description="Only when the status changes, not on every comment."
              checked={preferences.feedback_updates}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "feedback_updates", value }).catch(() => {})}
            />
            <Switch
              label="Product changelog"
              description="A short email when we ship something notable. Roughly monthly."
              checked={preferences.changelog}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "changelog", value }).catch(() => {})}
            />
            <Switch
              label="Satisfaction surveys"
              description="One short question after a request is resolved."
              checked={preferences.surveys}
              disabled={preference.isPending}
              onCheckedChange={(value) => void preference.mutate({ key: "surveys", value }).catch(() => {})}
            />
            {Boolean(preference.error) && <p className="text-xs text-danger">Could not update notification preferences. Please try again.</p>}
          </CardBody>
        </Card>
      </Section>

      <Section title="Your data">
        <Callout tone="info" className="mb-3">
          You can take a copy of everything associated with your account, or ask us to delete it.
          Deletion removes your messages and profile; anonymised aggregates we are required to keep
          for reporting cannot identify you.
        </Callout>

        <Card>
          <CardHeader
            title="Download your data"
            description="A machine-readable archive of your requests, messages, and profile."
            actions={
              <Button variant="secondary" size="sm" leading={<Download />}>
                Request archive
              </Button>
            }
          />
        </Card>

        <Card className="mt-3 border-danger-border">
          <CardHeader
            title="Delete your account"
            description="Removes your profile and closes your open requests. This cannot be undone."
            actions={
              <Button variant="danger" size="sm" leading={<Trash2 />}>
                Delete account
              </Button>
            }
          />
        </Card>
      </Section>
    </div>
  );
}
