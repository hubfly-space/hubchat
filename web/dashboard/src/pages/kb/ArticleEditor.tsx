import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  DetailRow,
  Field,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  Section,
  SegmentedControl,
  Select,
  Switch,
  Textarea,
  Toolbar,
  ToolbarDivider,
  Tooltip,
  cn,
  formatRelativeShort,
} from "@hubchat/shared";
import {
  Bold,
  Code2,
  Eye,
  Heading2,
  History,
  ImageIcon,
  Italic,
  Link2,
  List,
  ListOrdered,
  MoreHorizontal,
  Quote,
  Table2,
} from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { NOW, articles, collections } from "../../data/fixtures";

const SAMPLE = `Every webhook request Hubchat sends carries two headers:

    Hubchat-Signature: v1=5257a869e7ecebeda32affa62cdca3fa
    Hubchat-Timestamp: 1774526400

Verify both. The signature alone does not protect you from replay.

## Constructing the signed payload

Concatenate the timestamp, a dot, and the raw request body — the exact bytes you received, before any JSON parsing.

## Rejecting stale requests

Reject anything whose timestamp is more than five minutes old. Compare digests in constant time.`;

/**
 * Article editor (§6.8).
 *
 * Markdown, not a WYSIWYG surface. Help content is version-controlled, diffed,
 * imported from and exported to Markdown files, and frequently written by
 * engineers — a rich-text model would fight all four.
 */
export default function ArticleEditor() {
  const { articleId } = useParams();
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [body, setBody] = useState(SAMPLE);

  const article = articles.find((item) => item.id === articleId);
  const title = article?.title ?? "Untitled article";

  return (
    <Page>
      <Toolbar
        className="h-topbar py-0"
        leading={
          <>
            <span className="truncate text-sm font-medium text-fg">{title}</span>
            {article && <Badge tone="neutral">v{article.version}</Badge>}
            <span className="text-2xs text-fg-muted">
              Saved {article ? formatRelativeShort(article.updated_at, NOW) : "just now"} ago
            </span>
          </>
        }
        trailing={
          <>
            <SegmentedControl
              aria-label="Editor mode"
              value={mode}
              onValueChange={setMode}
              options={[
                { value: "write", label: "Write" },
                { value: "preview", label: "Preview", icon: <Eye /> },
              ]}
            />
            <ToolbarDivider />
            <Button variant="ghost" size="sm" leading={<History />}>
              History
            </Button>
            <Button variant="secondary" size="sm">
              Save draft
            </Button>
            <Menu>
              <MenuTrigger asChild>
                <Button variant="primary" size="sm">
                  Publish
                </Button>
              </MenuTrigger>
              <MenuContent align="end">
                <MenuLabel>Publish</MenuLabel>
                <MenuItem>Publish now</MenuItem>
                <MenuItem>Schedule for later…</MenuItem>
                <MenuItem>Send for review</MenuItem>
              </MenuContent>
            </Menu>
          </>
        }
      />

      <div className="flex min-h-0 flex-1">
        {/* Editor -------------------------------------------------------- */}
        <div className="flex min-w-0 flex-1 flex-col">
          {mode === "write" && (
            <div className="flex shrink-0 items-center gap-0.5 border-b border-line bg-surface px-3 py-1.5">
              {[
                { icon: <Heading2 />, label: "Heading" },
                { icon: <Bold />, label: "Bold" },
                { icon: <Italic />, label: "Italic" },
                { icon: <Link2 />, label: "Link" },
              ].map((tool) => (
                <Tooltip key={tool.label} content={tool.label}>
                  <Button variant="ghost" size="xs" iconOnly aria-label={tool.label} leading={tool.icon} />
                </Tooltip>
              ))}
              <ToolbarDivider />
              {[
                { icon: <List />, label: "Bulleted list" },
                { icon: <ListOrdered />, label: "Numbered list" },
                { icon: <Quote />, label: "Callout" },
                { icon: <Code2 />, label: "Code block" },
                { icon: <Table2 />, label: "Table" },
                { icon: <ImageIcon />, label: "Image" },
              ].map((tool) => (
                <Tooltip key={tool.label} content={tool.label}>
                  <Button variant="ghost" size="xs" iconOnly aria-label={tool.label} leading={tool.icon} />
                </Tooltip>
              ))}
            </div>
          )}

          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="mx-auto max-w-measure px-8 py-8">
              <input
                defaultValue={title}
                aria-label="Article title"
                className="mb-4 w-full bg-transparent text-3xl font-semibold tracking-tighter text-fg outline-none placeholder:text-fg-disabled"
                placeholder="Article title"
              />

              {mode === "write" ? (
                <Textarea
                  data-editor
                  value={body}
                  onChange={(event) => setBody(event.target.value)}
                  aria-label="Article body"
                  className={cn(
                    "min-h-[60vh] resize-none border-0 bg-transparent p-0 font-mono text-sm leading-relaxed",
                    "shadow-none focus:bg-transparent focus:shadow-none",
                  )}
                />
              ) : (
                <article className="prose-hubchat text-sm leading-relaxed text-fg-secondary">
                  {body.split("\n\n").map((block, index) =>
                    block.startsWith("## ") ? (
                      <h2 key={index} className="mb-2 mt-6 text-lg font-semibold text-fg">
                        {block.slice(3)}
                      </h2>
                    ) : block.startsWith("    ") ? (
                      <pre
                        key={index}
                        className="my-4 overflow-x-auto rounded-md border border-line bg-inset p-3 font-mono text-xs text-fg-secondary"
                      >
                        {block.replace(/^ {4}/gm, "")}
                      </pre>
                    ) : (
                      <p key={index} className="mb-4">
                        {block}
                      </p>
                    ),
                  )}
                </article>
              )}
            </div>
          </div>
        </div>

        {/* Metadata ------------------------------------------------------ */}
        <aside className="hidden w-context shrink-0 overflow-y-auto border-l border-line bg-surface p-4 lg:block">
          <Section title="Placement">
            <div className="flex flex-col gap-3">
              <Field label="Collection" htmlFor="collection">
                <Select
                  id="collection"
                  size="sm"
                  defaultValue={article?.collection_id ?? collections[0]?.id}
                  options={collections.map((collection) => ({
                    value: collection.id,
                    label: collection.name,
                  }))}
                  aria-label="Collection"
                />
              </Field>

              <Field label="Slug" htmlFor="slug" description="Changing this breaks existing links.">
                <Input id="slug" inputSize="sm" mono defaultValue={article?.slug} />
              </Field>

              <Field label="Language" htmlFor="language">
                <Select
                  id="language"
                  size="sm"
                  defaultValue="en"
                  options={[
                    { value: "en", label: "English" },
                    { value: "pt", label: "Português" },
                    { value: "ja", label: "日本語" },
                  ]}
                  aria-label="Language"
                />
              </Field>
            </div>
          </Section>

          <Section title="Visibility">
            <div className="flex flex-col gap-3">
              <Switch
                label="Public"
                description="Visible without signing in to the portal."
                defaultChecked
              />
              <Switch
                label="Show in widget search"
                description="Surfaces this article before the contact options."
                defaultChecked
              />
              <Switch label="Collect article feedback" defaultChecked />
            </div>
          </Section>

          <Section title="SEO">
            <div className="flex flex-col gap-3">
              <Field label="Meta title" htmlFor="seo-title">
                <Input id="seo-title" inputSize="sm" placeholder={title} />
              </Field>
              <Field
                label="Meta description"
                htmlFor="seo-desc"
                hint="155 max"
              >
                <Textarea id="seo-desc" rows={3} className="text-xs" />
              </Field>
            </div>
          </Section>

          {article && (
            <Card>
              <CardHeader
                title="Performance"
                actions={
                  <Button variant="ghost" size="xs" iconOnly aria-label="More" leading={<MoreHorizontal />} />
                }
              />
              <CardBody>
                <dl>
                  <DetailRow label="Views">{article.view_count.toLocaleString()}</DetailRow>
                  <DetailRow label="Helpful">{article.helpful_count}</DetailRow>
                  <DetailRow label="Not helpful">{article.unhelpful_count}</DetailRow>
                  <DetailRow label="Revisions">{article.version}</DetailRow>
                </dl>
              </CardBody>
            </Card>
          )}
        </aside>
      </div>
    </Page>
  );
}
