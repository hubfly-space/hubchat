import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Spinner,
  StatusDot,
  type Message as SharedMessage,
} from "@hubchat/shared";
import { useEffect, useRef, useState } from "react";
import * as api from "../../lib/api";
import { Composer } from "../inbox/Composer";
import { MessageTimeline } from "../inbox/MessageTimeline";

/**
 * Live diagnostic demo. It exercises the real Go backend and PostgreSQL from
 * inside the dashboard, including auth, workspace creation, messaging, and
 * realtime delivery. Product routes use their live APIs; this route remains
 * deliberately separate from the normal navigation because it creates test
 * data and is intended for installation diagnostics.
 *
 * Intentionally outside AppShell (see router.tsx) — it is a diagnostic tool,
 * not a product surface, and doesn't pretend to be one.
 */
export default function LiveDemo() {
  const [stage, setStage] = useState<"auth" | "workspace" | "ready">("auth");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [email, setEmail] = useState("demo@northwind.cloud");
  const [password, setPassword] = useState("correct-horse-battery");
  const [name, setName] = useState("Demo Agent");

  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const [memberId, setMemberId] = useState<string | null>(null);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [messages, setMessages] = useState<SharedMessage[]>([]);
  const [connectionState, setConnectionState] = useState<"connecting" | "open" | "closed">("connecting");

  const disconnectRef = useRef<() => void>(() => undefined);

  const runAuth = async (mode: "signup" | "login") => {
    setBusy(true);
    setError(null);
    try {
      if (mode === "signup") {
        await api.signUp(name, email, password);
      } else {
        await api.logIn(email, password);
      }

      // A fresh account has no workspace yet; an existing demo account
      // usually does. /auth/me tells us which state we're in.
      try {
        const identity = await api.me();
        setWorkspaceId(identity.workspace.id);
        setMemberId(identity.member_id);
        setStage("ready");
      } catch {
        setStage("workspace");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  };

  const createWorkspace = async () => {
    setBusy(true);
    setError(null);
    try {
      const ws = await api.createWorkspace("Live Demo Workspace", `demo-${Date.now().toString(36)}`);
      const identity = await api.me(ws.id);
      setWorkspaceId(ws.id);
      setMemberId(identity.member_id);
      setStage("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create a workspace.");
    } finally {
      setBusy(false);
    }
  };

  const startConversation = async () => {
    if (!workspaceId) return;
    setBusy(true);
    setError(null);
    try {
      // The workspace bootstrap always provisions one default inbox (see
      // internal/workspace.Service.Bootstrap); look it up the same way the
      // curl verification did, through the conversation list for a
      // known-empty inbox would require an inbox id we don't have client-side
      // yet, so this demo starts a conversation directly against the
      // workspace's only inbox by convention. A real inbox picker belongs to
      // the channels/inboxes screen, not this diagnostic page.
      const { data: inboxes } = await api.listInboxes(workspaceId);
      const inbox = inboxes.find((i) => i.is_default) ?? inboxes[0];
      if (!inbox) throw new Error("This workspace has no inbox yet.");

      const result = await api.startConversation(workspaceId, {
        inboxId: inbox.id,
        channel: "widget",
        authorName: "Live demo visitor",
        body: "Hello — this conversation was created by the live-demo page.",
      });
      setConversationId(result.conversation.id);
      setMessages([toSharedMessage(result.message)]);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start a conversation.");
    } finally {
      setBusy(false);
    }
  };

  // Realtime subscription — reconnects whenever the workspace changes.
  useEffect(() => {
    if (!workspaceId) return;

    setConnectionState("connecting");
    const disconnect = api.connectRealtime(
      workspaceId,
      (envelope) => {
        setConnectionState("open");
        if (envelope.type !== "message.created") return;

        const data = envelope.data as { conversation_id: string; message: api.ApiMessage };
        setConversationId((current) => {
          if (current && data.conversation_id !== current) return current;
          return current;
        });

        setMessages((current) => {
          if (current.some((m) => m.id === data.message.id)) return current;
          return [...current, toSharedMessage(data.message)];
        });
      },
      () => setConnectionState("closed"),
    );

    disconnectRef.current = disconnect;
    return disconnect;
  }, [workspaceId]);

  const onSend = async (body: string, kind: "reply" | "note") => {
    if (!workspaceId || !conversationId) return;
    await api.postMessage(workspaceId, conversationId, {
      kind,
      authorName: name || "Demo Agent",
      body,
      idempotencyKey: `${conversationId}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
    });
    // Deliberately not appending optimistically here: the realtime handler
    // above appends once the server confirms, which is the same path a
    // second browser tab watching this conversation would take. That is the
    // behaviour worth demonstrating on this page.
  };

  return (
    <Page>
      <PageHeader
        title="Live backend demo"
        description="Talks to the real Go API and PostgreSQL — not fixtures. See web/dashboard/src/lib/api.ts."
        meta={
          workspaceId && (
            <Badge tone={connectionState === "open" ? "success" : connectionState === "connecting" ? "warning" : "danger"}>
              <StatusDot status={connectionState === "open" ? "live" : "offline"} className="mr-1" />
              {connectionState}
            </Badge>
          )
        }
      />

      <PageBody width="narrow">
        {error && (
          <Callout tone="danger" className="mb-4">
            {error}
          </Callout>
        )}

        {stage === "auth" && (
          <Card>
            <CardBody className="space-y-4">
              <Field label="Name (for sign-up)">
                <Input value={name} onChange={(e) => setName(e.target.value)} />
              </Field>
              <Field label="Email">
                <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" />
              </Field>
              <Field label="Password">
                <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" />
              </Field>
              <div className="flex gap-2">
                <Button variant="primary" loading={busy} onClick={() => runAuth("signup")}>
                  Sign up
                </Button>
                <Button variant="secondary" loading={busy} onClick={() => runAuth("login")}>
                  Log in
                </Button>
              </div>
            </CardBody>
          </Card>
        )}

        {stage === "workspace" && (
          <Card>
            <CardBody className="space-y-3">
              <p className="text-sm text-fg">Signed in. This account has no workspace yet.</p>
              <Button variant="primary" loading={busy} onClick={createWorkspace}>
                Create a workspace
              </Button>
            </CardBody>
          </Card>
        )}

        {stage === "ready" && workspaceId && (
          <div className="space-y-4">
            <Callout tone="info">
              Workspace <code className="font-mono">{workspaceId}</code> · member{" "}
              <code className="font-mono">{memberId}</code>
            </Callout>

            {!conversationId ? (
              <Button variant="primary" loading={busy} onClick={startConversation}>
                Start a conversation
              </Button>
            ) : (
              <Card className="flex h-[520px] flex-col overflow-hidden p-0">
                <div className="min-h-0 flex-1 overflow-y-auto">
                  {messages.length === 0 ? (
                    <div className="flex h-full items-center justify-center text-fg-muted">
                      <Spinner />
                    </div>
                  ) : (
                    <MessageTimeline messages={messages} />
                  )}
                </div>
                <Composer conversationId={conversationId} customerName="Live demo visitor" onSend={onSend} />
              </Card>
            )}
          </div>
        )}
      </PageBody>
    </Page>
  );
}

function toSharedMessage(message: api.ApiMessage): SharedMessage {
  return {
    id: message.id,
    client_id: message.client_id,
    conversation_id: message.conversation_id,
    workspace_id: "",
    kind: message.kind,
    author_type: message.author_type,
    author_id: message.author_id,
    author_name: message.author_name,
    author_avatar_url: null,
    body: message.body,
    event_type: null,
    event_data: null,
    attachments: [],
    quoted_message_id: null,
    delivery: "sent",
    edited_at: null,
    redacted_at: null,
    sequence: message.sequence,
    created_at: message.created_at,
  };
}
