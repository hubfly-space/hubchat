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
} from "@hubchat/shared";
import { Download, Trash2, UserRound } from "lucide-react";
import { usePortal } from "../portal-context";
import { Link } from "react-router-dom";

/** Customer profile and communication preferences (§6.5). */
export default function Account() {
  const { data } = usePortal();
  if (!data?.viewer) return <EmptyState icon={UserRound} title="Sign in to view your profile" description="Your portal profile is available after email sign-in." action={<Button variant="primary" size="sm" asChild><Link to="/sign-in">Sign in</Link></Button>} />;
  const viewer = data.viewer;
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
          <CardBody className="pt-0">
            <SettingsRow label="Photo">
              <div className="flex items-center gap-3">
                <Avatar name={viewer.name} seed={viewer.email} size="lg" />
                <Button variant="secondary" size="sm">
                  Upload
                </Button>
              </div>
            </SettingsRow>

            <SettingsRow label="Name" htmlFor="name">
              <Input id="name" inputSize="sm" defaultValue={viewer.name} />
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
          </CardBody>
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
              defaultChecked
            />
            <Switch
              label="Updates on feedback I follow"
              description="Only when the status changes, not on every comment."
              defaultChecked
            />
            <Switch
              label="Product changelog"
              description="A short email when we ship something notable. Roughly monthly."
            />
            <Switch
              label="Satisfaction surveys"
              description="One short question after a request is resolved."
              defaultChecked
            />
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
