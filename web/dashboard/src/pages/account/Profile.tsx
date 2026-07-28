import {
  api,
  ApiError,
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CopyField,
  Dialog,
  DialogClose,
  DialogContent,
  Field,
  Input,
  invalidate,
  Page,
  PageBody,
  PageHeader,
  Section,
  SettingsRow,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

type TotpStatus = { enabled: boolean; remaining_recovery_codes: number };

/** Personal profile (§11.1). */
export default function Profile() {
  const { viewer } = useWorkspace();

  const [name, setName] = useState(viewer.name);
  const nameDirty = name.trim() !== viewer.name;

  const saveProfile = useMutation<{ name: string }, unknown>(
    (body) => api.patch("/auth/profile", body),
    { invalidates: [["bootstrap"]] },
  );

  return (
    <Page>
      <PageHeader
        title="Profile"
        description="How you appear to your team and, where permitted, to customers."
        actions={
          <Button
            variant="primary"
            size="sm"
            disabled={!nameDirty}
            loading={saveProfile.isPending}
            onClick={() => void saveProfile.mutate({ name: name.trim() })}
          >
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        {saveProfile.error ? (
          <Callout tone="danger" className="mb-4">
            {saveProfile.error instanceof ApiError
              ? saveProfile.error.message
              : "Could not save your profile."}
          </Callout>
        ) : null}
        {saveProfile.isSuccess && (
          <Callout tone="success" className="mb-4">
            Saved.
          </Callout>
        )}

        <Section title="Identity">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Avatar"
                description="Shown on your replies. Generated from your name until avatar uploads land."
              >
                <Avatar name={viewer.name} seed={viewer.id} size="xl" />
              </SettingsRow>

              <SettingsRow label="Full name" htmlFor="name">
                <Input
                  id="name"
                  inputSize="sm"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Email">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Primary email" description="Used for sign-in and notifications.">
                <div className="flex items-center gap-2">
                  <Input inputSize="sm" defaultValue={viewer.email} disabled />
                  <Badge tone="success" leading={<ShieldCheck />}>
                    Verified
                  </Badge>
                </div>
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <PasswordSection />
        <TwoFactorSection />

        <Section title="Workspace">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Role" description="Only an owner or admin can change this.">
                <Badge tone="accent">{viewer.role}</Badge>
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}

function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [mismatch, setMismatch] = useState(false);

  const change = useMutation<{ current_password: string; new_password: string }, unknown>(
    (body) => api.patch("/auth/password/change", body),
    {
      onSuccess: () => {
        setCurrent("");
        setNext("");
        setConfirm("");
      },
    },
  );

  const submit = () => {
    if (next !== confirm) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    void change.mutate({ current_password: current, new_password: next }).catch(() => {});
  };

  return (
    <Section title="Password">
      <Card>
        <CardBody className="space-y-4">
          <Callout tone="info">
            Changing your password signs out every other session on your account. This one stays
            signed in.
          </Callout>

          {change.error ? (
            <Callout tone="danger">
              {change.error instanceof ApiError ? change.error.message : "Could not change your password."}
            </Callout>
          ) : null}
          {mismatch && <Callout tone="warning">The new passwords do not match.</Callout>}
          {change.isSuccess && <Callout tone="success">Password changed.</Callout>}

          <SettingsRow label="Current password" htmlFor="current">
            <Input
              id="current"
              inputSize="sm"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(event) => setCurrent(event.target.value)}
            />
          </SettingsRow>
          <SettingsRow label="New password" htmlFor="new">
            <Input
              id="new"
              inputSize="sm"
              type="password"
              autoComplete="new-password"
              minLength={12}
              value={next}
              onChange={(event) => setNext(event.target.value)}
            />
          </SettingsRow>
          <SettingsRow label="Confirm new password" htmlFor="confirm">
            <Input
              id="confirm"
              inputSize="sm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(event) => setConfirm(event.target.value)}
            />
          </SettingsRow>

          <div className="flex justify-end">
            <Button
              variant="primary"
              size="sm"
              loading={change.isPending}
              disabled={!current || next.length < 12}
              onClick={submit}
            >
              Change password
            </Button>
          </div>
        </CardBody>
      </Card>
    </Section>
  );
}

function TwoFactorSection() {
  const status = useQuery<TotpStatus>(["totp-status"], (signal) =>
    api.get<TotpStatus>("/auth/totp", { signal }),
  );
  const [enrolling, setEnrolling] = useState(false);
  const [disabling, setDisabling] = useState(false);

  return (
    <Section title="Two-factor authentication">
      <Card>
        <CardHeader
          title="Authenticator app"
          description="A time-based code from any authenticator, plus ten single-use recovery codes."
          actions={
            <Badge tone={status.data?.enabled ? "success" : "neutral"}>
              {status.data?.enabled ? "Enabled" : "Not enabled"}
            </Badge>
          }
        />
        <CardBody className="flex flex-wrap items-center gap-2">
          {status.data?.enabled ? (
            <>
              <span className="text-xs text-fg-muted">
                {status.data.remaining_recovery_codes} recovery codes remaining
              </span>
              <Button
                variant="danger-ghost"
                size="sm"
                leading={<ShieldOff />}
                onClick={() => setDisabling(true)}
              >
                Disable two-factor
              </Button>
            </>
          ) : (
            <Button variant="secondary" size="sm" onClick={() => setEnrolling(true)}>
              Enable two-factor
            </Button>
          )}
        </CardBody>
      </Card>

      {enrolling && <EnrollDialog onClose={() => setEnrolling(false)} />}
      {disabling && <DisableDialog onClose={() => setDisabling(false)} />}
    </Section>
  );
}

type BeginResponse = { secret: string; provisioning_uri: string };

function EnrollDialog({ onClose }: { onClose: () => void }) {
  const [code, setCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);

  const begin = useQuery<BeginResponse>(["totp-begin"], (signal) =>
    api.post<BeginResponse>("/auth/totp/begin", undefined, { signal }),
    { staleTime: Infinity },
  );

  const complete = useMutation<{ secret: string; code: string }, { recovery_codes: string[] }>(
    (body) => api.post("/auth/totp/complete", body),
    {
      onSuccess: (result) => {
        setRecoveryCodes(result.recovery_codes);
        invalidate(["totp-status"]);
      },
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={recoveryCodes ? "Save your recovery codes" : "Set up two-factor authentication"}
        hideClose={!recoveryCodes}
        footer={
          recoveryCodes ? (
            <Button variant="primary" size="sm" onClick={onClose}>
              I've saved these codes
            </Button>
          ) : (
            <>
              <DialogClose asChild>
                <Button variant="ghost" size="sm">
                  Cancel
                </Button>
              </DialogClose>
              <Button
                variant="primary"
                size="sm"
                loading={complete.isPending}
                disabled={code.length !== 6 || !begin.data}
                onClick={() => begin.data && void complete.mutate({ secret: begin.data.secret, code })}
              >
                Verify and enable
              </Button>
            </>
          )
        }
      >
        {recoveryCodes ? (
          <div className="flex flex-col gap-3">
            <Callout tone="warning">
              These are shown once. Store them somewhere safe — each can be used in place of your
              authenticator code, one time only.
            </Callout>
            <div className="grid grid-cols-2 gap-2 font-mono text-sm">
              {recoveryCodes.map((recoveryCode) => (
                <span key={recoveryCode} className="rounded-md border border-line bg-inset px-2 py-1">
                  {recoveryCode}
                </span>
              ))}
            </div>
          </div>
        ) : begin.data ? (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-fg-muted">
              Add this account to your authenticator app, then enter the six-digit code it shows.
            </p>
            <CopyField label="Secret" value={begin.data.secret} />
            <p className="text-2xs text-fg-disabled">
              Most apps can also import via a QR code — scan this link with your phone's camera, or
              paste it into an authenticator that accepts setup links:
            </p>
            <CopyField value={begin.data.provisioning_uri} />
            <Field label="Verification code" htmlFor="totp-code">
              <Input
                id="totp-code"
                mono
                inputSize="lg"
                maxLength={6}
                inputMode="numeric"
                autoFocus
                value={code}
                onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))}
              />
            </Field>
            {complete.error ? (
              <Callout tone="danger">
                {complete.error instanceof ApiError ? complete.error.message : "Could not verify that code."}
              </Callout>
            ) : null}
          </div>
        ) : begin.error ? (
          <Callout tone="danger">
            {begin.error instanceof ApiError ? begin.error.message : "Could not start enrolment."}
          </Callout>
        ) : (
          <p className="text-sm text-fg-muted">Preparing…</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

function DisableDialog({ onClose }: { onClose: () => void }) {
  const [password, setPassword] = useState("");

  const disable = useMutation<{ password: string }, unknown>(
    (body) => api.post("/auth/totp/disable", body),
    {
      onSuccess: () => {
        invalidate(["totp-status"]);
        onClose();
      },
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="Disable two-factor authentication"
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="danger"
              size="sm"
              loading={disable.isPending}
              disabled={!password}
              onClick={() => void disable.mutate({ password }).catch(() => {})}
            >
              Disable
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          <Callout tone="warning">
            This removes the second factor protecting your account. Confirm with your password.
          </Callout>
          {disable.error ? (
            <Callout tone="danger">
              {disable.error instanceof ApiError ? disable.error.message : "That password is incorrect."}
            </Callout>
          ) : null}
          <Field label="Current password" htmlFor="disable-password">
            <Input
              id="disable-password"
              type="password"
              inputSize="lg"
              autoComplete="current-password"
              autoFocus
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}
