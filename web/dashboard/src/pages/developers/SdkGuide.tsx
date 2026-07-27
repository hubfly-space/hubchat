import {
  Badge,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CodeBlock,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tabs,
  TabsContent,
  TabsList,
} from "@hubchat/shared";
import { ShieldAlert } from "lucide-react";
import { useState } from "react";

const SDK_METHODS = [
  { name: "boot", signature: "Hubchat('boot', { key })", detail: "Loads the widget. Safe to call once per page." },
  { name: "identify", signature: "Hubchat('identify', { external_id, email, token })", detail: "Attaches a verified customer to the session." },
  { name: "update", signature: "Hubchat('update', attributes)", detail: "Sets allowlisted customer attributes." },
  { name: "track", signature: "Hubchat('track', type, payload)", detail: "Records a declared application event." },
  { name: "show / hide", signature: "Hubchat('show')", detail: "Opens or closes the panel." },
  { name: "startConversation", signature: "Hubchat('startConversation', { message })", detail: "Opens straight into a new thread." },
  { name: "openArticle", signature: "Hubchat('openArticle', slug)", detail: "Deep-links to a help article." },
  { name: "openForm", signature: "Hubchat('openForm', slug)", detail: "Opens a ticket or feedback form." },
  { name: "reset", signature: "Hubchat('reset')", detail: "Clears the visitor session. Call on sign-out." },
  { name: "on", signature: "Hubchat('on', event, handler)", detail: "Subscribes to widget lifecycle events." },
];

/** SDK and installation reference (§6.16). */
export default function SdkGuide() {
  const [tab, setTab] = useState("install");

  return (
    <Page>
      <PageHeader
        title="SDK & installation"
        description="Everything needed to embed Hubchat and feed it context from your application."
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "install", label: "Install" },
                { value: "identity", label: "Identity" },
                { value: "reference", label: "Reference" },
                { value: "api", label: "REST API" },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody width="narrow">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="install">
            <Section
              title="Script tag"
              description="The loader is under 2 KB gzipped and fetches the interface lazily. It never blocks your page render."
            >
              <CodeBlock
                filename="index.html"
                code={`<script>
  (function(h,u,b){h.Hubchat=h.Hubchat||function(){(h.Hubchat.q=h.Hubchat.q||[]).push(arguments)};
  var s=u.createElement('script');s.async=1;s.src=b;u.head.appendChild(s)})
  (window,document,'https://support.northwind.cloud/widget/v1.js');

  Hubchat('boot', { key: 'pk_live_8f2a41cd9b7e' });
</script>`}
              />
            </Section>

            <Section title="React">
              <CodeBlock language="bash" code="npm install @hubchat/widget" />
              <CodeBlock
                className="mt-2"
                language="tsx"
                code={`import { HubchatWidget, useHubchat } from '@hubchat/widget/react';

export function SupportButton() {
  const hubchat = useHubchat();
  return <button onClick={() => hubchat.startConversation()}>Contact support</button>;
}

export function App() {
  return (
    <>
      <Routes />
      <HubchatWidget publicKey="pk_live_8f2a41cd9b7e" identity={{ token }} />
    </>
  );
}`}
              />
            </Section>

            <Section title="Content Security Policy">
              <p className="mb-2 text-xs text-fg-muted">
                If your site sets a CSP, the widget needs these directives.
              </p>
              <CodeBlock
                language="text"
                code={`script-src  https://support.northwind.cloud
connect-src https://support.northwind.cloud wss://support.northwind.cloud
img-src     https://support.northwind.cloud data:
style-src   'unsafe-inline'   # widget styles are injected into a shadow root`}
              />
            </Section>
          </TabsContent>

          <TabsContent value="identity">
            <Callout tone="danger" className="mb-5" icon={<ShieldAlert />} title="Never sign tokens in the browser">
              A customer ID sent from the browser is a claim, not proof. Anyone can open the console
              and claim to be anyone. Sign the token on your server with a secret the browser never
              sees — this is the difference between a Verified and an Unverified badge in the agent's
              context panel, and agents are trained to treat them differently.
            </Callout>

            <Section title="1. Generate the token on your server">
              <CodeBlock
                language="go"
                filename="identity.go"
                code={`func HubchatIdentityToken(secret string, customerID, email string) string {
	claims := map[string]any{
		"workspace_id": "wrk_01hq7x",
		"external_id":  customerID,
		"email":        email,
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(time.Hour).Unix(),
		"jti":          uuid.NewString(), // single-use nonce
	}
	payload, _ := json.Marshal(claims)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}`}
              />
            </Section>

            <Section title="2. Pass it to the SDK">
              <CodeBlock
                language="javascript"
                code={`Hubchat('identify', {
  external_id: 'u_44192',
  email: 'mariana@atlasfreight.com',
  token: window.__HUBCHAT_TOKEN__, // rendered by your server
});`}
              />
            </Section>

            <Section title="3. Reset on sign-out">
              <p className="mb-2 text-xs text-fg-muted">
                Without this, the next person to use the same browser inherits the previous
                customer's conversation history.
              </p>
              <CodeBlock language="javascript" code="Hubchat('reset');" />
            </Section>

            <Section title="What Hubchat verifies">
              <Card>
                <CardBody>
                  <ul className="space-y-2 text-sm text-fg-secondary">
                    {[
                      "The HMAC signature against the workspace secret",
                      "That the workspace ID in the token matches the widget's workspace",
                      "That the token was issued in the past and has not expired",
                      "That the nonce has not been seen before, within the replay window",
                      "That the external ID has not been claimed by a different verified identity",
                    ].map((check) => (
                      <li key={check} className="flex gap-2">
                        <span className="mt-1.5 size-1 shrink-0 rounded-full bg-accent" />
                        {check}
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>
          </TabsContent>

          <TabsContent value="reference">
            <Section title="Methods">
              <Card>
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {SDK_METHODS.map((method) => (
                      <li key={method.name} className="px-4 py-3">
                        <p className="font-mono text-xs text-fg">{method.signature}</p>
                        <p className="mt-1 text-xs text-fg-muted">{method.detail}</p>
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section title="Lifecycle events">
              <CodeBlock
                language="javascript"
                code={`Hubchat('on', 'ready',              () => {});
Hubchat('on', 'open',               () => {});
Hubchat('on', 'close',              () => {});
Hubchat('on', 'conversation:started', ({ id }) => {});
Hubchat('on', 'message:received',   ({ id, body }) => {});
Hubchat('on', 'unread:changed',     ({ count }) => {});`}
              />
            </Section>
          </TabsContent>

          <TabsContent value="api">
            <Section title="Conventions">
              <Card>
                <CardHeader title="Every endpoint under /api/v1" />
                <CardBody>
                  <ul className="space-y-2 text-sm text-fg-secondary">
                    {[
                      ["Authentication", "Bearer token, workspace-scoped"],
                      ["Pagination", "Cursor-based; never page numbers"],
                      ["Idempotency", "Idempotency-Key header on every create"],
                      ["Tracing", "X-Request-Id echoed on every response"],
                      ["Rate limits", "X-RateLimit-Limit, -Remaining, -Reset"],
                      ["Errors", "One shape, always: { error: { code, message, request_id } }"],
                    ].map(([label, detail]) => (
                      <li key={label} className="flex items-start gap-3">
                        <Badge tone="neutral" className="mt-0.5 shrink-0">
                          {label}
                        </Badge>
                        <span className="text-xs">{detail}</span>
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section title="Example">
              <CodeBlock
                language="bash"
                code={`curl -X POST https://support.northwind.cloud/api/v1/tickets \\
  -H "Authorization: Bearer hc_live_9f2a…" \\
  -H "Idempotency-Key: 0d4f1a2c-9b3e-4f10-a5c2-77b1de3a9e04" \\
  -H "Content-Type: application/json" \\
  -d '{
    "title": "Webhook deliveries failing",
    "customer": { "external_id": "u_44192" },
    "priority": "urgent",
    "inbox_id": "inb_support"
  }'`}
              />
            </Section>

            <Section title="Error shape">
              <CodeBlock
                language="json"
                code={`{
  "error": {
    "code": "ticket_not_found",
    "message": "The requested ticket was not found.",
    "request_id": "req_01hq7xk2m9",
    "details": {}
  }
}`}
              />
            </Section>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
