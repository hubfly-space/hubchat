import {
  Button,
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
import { Download, Upload } from "lucide-react";
import { useState } from "react";

const IMPORTERS = [
  { key: "customers", label: "Customers", detail: "CSV with email, name, external ID, and any declared attributes." },
  { key: "companies", label: "Companies", detail: "CSV with name, domain, external ID, and tier." },
  { key: "tickets", label: "Tickets", detail: "CSV with subject, body, status, and requester email." },
  { key: "articles", label: "Knowledge-base articles", detail: "Markdown files with front matter, or a zip of them." },
  { key: "feedback", label: "Feedback items", detail: "CSV with title, description, and vote count." },
];

const EXPORTS = [
  { key: "workspace", label: "Full workspace archive", detail: "Everything below, plus settings and an attachment manifest." },
  { key: "conversations", label: "Conversations and messages", detail: "JSONL, one conversation per line." },
  { key: "tickets", label: "Tickets", detail: "CSV or JSONL, including custom field values." },
  { key: "customers", label: "Customers and companies", detail: "CSV. Sensitive fields are excluded unless you hold the capability." },
  { key: "kb", label: "Knowledge base", detail: "Markdown files, ready to re-import elsewhere." },
  { key: "audit", label: "Audit log", detail: "JSONL, append-only ordering preserved." },
];

/** Import, export, and portability (§6.20). */
export default function ImportExport() {
  const [tab, setTab] = useState("export");

  return (
    <Page>
      <PageHeader
        title="Import & export"
        description="Your data is yours. Everything Hubchat stores can leave in a documented format."
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "export", label: "Export" },
                { value: "import", label: "Import" },
                { value: "backups", label: "Backups" },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody width="narrow">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="export">
            <Callout tone="info" className="mb-4">
              Exports run as background jobs and email you a signed download link when ready. Links
              expire after 24 hours; the archive itself is deleted after 7 days.
            </Callout>

            <Section title="In progress">
              <Card>
                <CardBody>
                  <p className="text-sm text-fg">No export jobs are currently running.</p>
                  <p className="mt-1 text-xs text-fg-muted">New exports will appear here with their verified row counts, attachment manifest, checksums, and expiry.</p>
                </CardBody>
              </Card>
            </Section>

            <Section title="Start an export">
              <div className="space-y-2">
                {EXPORTS.map((item) => (
                  <Card key={item.key}>
                    <CardBody className="flex items-center gap-4">
                      <div className="min-w-0 flex-1">
                        <p className="text-sm text-fg">{item.label}</p>
                        <p className="mt-0.5 text-xs text-fg-muted">{item.detail}</p>
                      </div>
                      <Button variant="secondary" size="sm" leading={<Download />}>
                        Export
                      </Button>
                    </CardBody>
                  </Card>
                ))}
              </div>
            </Section>

            <Section title="From the command line">
              <CodeBlock
                language="bash"
                code={`hubchat workspace export --slug northwind --out ./northwind.tar.zst
hubchat workspace import --file ./northwind.tar.zst --slug northwind-restored`}
              />
            </Section>
          </TabsContent>

          <TabsContent value="import">
            <Callout tone="warning" className="mb-4">
              Every import runs in preview first. You see the parsed rows, the columns Hubchat
              matched, and the ones it could not, before anything is written.
            </Callout>

            <Section title="Import data">
              <div className="space-y-2">
                {IMPORTERS.map((importer) => (
                  <Card key={importer.key}>
                    <CardBody className="flex items-center gap-4">
                      <div className="min-w-0 flex-1">
                        <p className="text-sm text-fg">{importer.label}</p>
                        <p className="mt-0.5 text-xs text-fg-muted">{importer.detail}</p>
                      </div>
                      <Button variant="secondary" size="sm" leading={<Upload />}>
                        Choose file
                      </Button>
                    </CardBody>
                  </Card>
                ))}
              </div>
            </Section>

            <Section title="Matching existing records">
              <Card>
                <CardHeader
                  title="How duplicates are handled"
                  description="Hubchat never merges on weak signals like a similar name (§26.3)."
                />
                <CardBody>
                  <ul className="space-y-2 text-xs text-fg-secondary">
                    <li>
                      <span className="text-fg">External ID</span> — an exact match updates the
                      existing record.
                    </li>
                    <li>
                      <span className="text-fg">Verified email</span> — an exact match updates, and
                      the import is recorded as the verification source.
                    </li>
                    <li>
                      <span className="text-fg">Anything else</span> — creates a new record. You can
                      merge manually afterwards with a preview.
                    </li>
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section title="Example CSV">
              <CodeBlock
                filename="customers.csv"
                code={`external_id,email,name,plan,seats,region
u_44192,mariana@atlasfreight.com,Mariana Costa,enterprise,240,eu
u_88103,d.osei@orbital.dev,Daniel Osei,enterprise,410,apac`}
              />
            </Section>
          </TabsContent>

          <TabsContent value="backups">
            <Callout tone="info" className="mb-4">
              Hubchat does not take your backups for you on a self-hosted deployment — your
              PostgreSQL and file storage are yours to snapshot. What it does provide is a logical
              export that is portable across versions.
            </Callout>

            <Section title="What to back up">
              <Card>
                <CardBody>
                  <ul className="space-y-3 text-sm">
                    <li>
                      <p className="text-fg">PostgreSQL database</p>
                      <p className="mt-0.5 text-xs text-fg-muted">
                        The source of truth. Everything except attachment bytes lives here.
                      </p>
                    </li>
                    <li>
                      <p className="text-fg">File storage</p>
                      <p className="mt-0.5 text-xs text-fg-muted">
                        The data directory, or your S3 bucket. Attachments and generated exports.
                      </p>
                    </li>
                    <li>
                      <p className="text-fg">Secret key</p>
                      <p className="mt-0.5 text-xs text-fg-muted">
                        Without <code className="font-mono">HUBCHAT_SECRET_KEY</code>, encrypted
                        integration secrets in a restored database cannot be decrypted.
                      </p>
                    </li>
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section title="Restore procedure">
              <CodeBlock
                language="bash"
                code={`# 1. Restore the database
pg_restore --clean --if-exists -d hubchat hubchat-2026-07-26.dump

# 2. Restore files
rsync -a backup/files/ /var/lib/hubchat/files/

# 3. Verify before serving traffic
hubchat migrate status
hubchat doctor --json`}
              />
            </Section>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
