import {
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tooltip,
  formatRelativeShort,
} from "@hubchat/shared";
import { CheckCircle2, ExternalLink, Globe, Plus, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { NOW, portals } from "../../data/fixtures";

const DOMAIN_STATUS = {
  active: { label: "Verified", tone: "success" },
  pending: { label: "Verifying", tone: "warning" },
  unverified: { label: "Not verified", tone: "neutral" },
  error: { label: "DNS error", tone: "danger" },
} as const;

/** Customer portals (§6.5). */
export default function PortalList() {
  return (
    <Page>
      <PageHeader
        title="Portals"
        description="Hosted, branded sites where customers submit tickets, track history, and browse help."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New portal
          </Button>
        }
      />

      <PageBody>
        <Section>
          {portals.length === 0 ? (
            <EmptyState
              icon={Globe}
              title="No portals yet"
              description="A portal gives customers a place to come back to — ticket history, guides, and a roadmap under your own domain."
            />
          ) : (
            <div className="space-y-3">
              {portals.map((portal) => {
                const status = DOMAIN_STATUS[portal.domain_status];
                return (
                  <Card key={portal.id}>
                    <CardBody>
                      <div className="flex flex-wrap items-start gap-4">
                        <span
                          aria-hidden="true"
                          className="mt-0.5 size-9 shrink-0 rounded-lg border border-line-strong"
                          style={{ backgroundColor: portal.theme.accent }}
                        />

                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <Link
                              to={`/channels/portals/${portal.id}`}
                              className="truncate text-sm font-medium text-fg hover:underline"
                            >
                              {portal.name}
                            </Link>
                            {!portal.enabled && <Badge tone="warning">Disabled</Badge>}
                          </div>

                          <div className="mt-1.5 flex flex-wrap items-center gap-2">
                            <a
                              href={`https://${portal.custom_domain ?? `${portal.subdomain}.hubchat.app`}`}
                              target="_blank"
                              rel="noreferrer"
                              className="inline-flex items-center gap-1 font-mono text-xs text-accent-text hover:underline"
                            >
                              {portal.custom_domain ?? `${portal.subdomain}.hubchat.app`}
                              <ExternalLink aria-hidden="true" className="size-3" />
                            </a>
                            <Tooltip
                              content={
                                portal.domain_status === "active"
                                  ? "DNS verified and TLS issued"
                                  : "Add the CNAME record shown in portal settings"
                              }
                            >
                              <span>
                                <Badge
                                  tone={status.tone}
                                  leading={portal.domain_status === "active" ? <CheckCircle2 /> : undefined}
                                >
                                  {status.label}
                                </Badge>
                              </span>
                            </Tooltip>
                          </div>

                          <p className="mt-2 text-xs text-fg-muted">
                            {Object.entries(portal.features)
                              .filter(([, enabled]) => enabled)
                              .map(([key]) => key.replace(/_/g, " "))
                              .join(" · ")}{" "}
                            · updated {formatRelativeShort(portal.updated_at, NOW)} ago
                          </p>
                        </div>

                        <Button variant="secondary" size="sm" leading={<Settings2 />} asChild>
                          <Link to={`/channels/portals/${portal.id}`}>Customise</Link>
                        </Button>
                      </div>
                    </CardBody>
                  </Card>
                );
              })}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
