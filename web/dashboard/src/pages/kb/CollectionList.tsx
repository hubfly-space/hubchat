import {
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
} from "@hubchat/shared";
import { Boxes, GripVertical, Plus, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { collections } from "../../data/fixtures";

/**
 * Collections (§6.8) — the navigation customers actually browse.
 *
 * Order is meaningful and drag-reorderable, because a help centre's first
 * collection is doing most of the work.
 */
export default function CollectionList() {
  return (
    <Page>
      <PageHeader
        title="Collections"
        description="How articles are grouped in the portal and widget. Drag to reorder."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New collection
          </Button>
        }
      />

      <PageBody>
        <Section>
          {collections.length === 0 ? (
            <EmptyState
              icon={Boxes}
              title="No collections"
              description="Group related articles so customers can browse rather than only search."
            />
          ) : (
            <div className="space-y-2">
              {collections.map((collection) => (
                <Card key={collection.id}>
                  <CardBody className="flex items-center gap-3">
                    <button
                      type="button"
                      aria-label={`Reorder ${collection.name}`}
                      className="cursor-grab rounded-sm p-1 text-fg-disabled transition-colors hover:bg-fill hover:text-fg-muted"
                    >
                      <GripVertical aria-hidden="true" className="size-4" />
                    </button>

                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-fg">{collection.name}</p>
                      <p className="mt-0.5 truncate text-xs text-fg-muted">
                        {collection.description} · /{collection.slug}
                      </p>
                    </div>

                    <span className="shrink-0 text-xs tabular text-fg-muted">
                      {collection.article_count} articles
                    </span>

                    <Button variant="ghost" size="sm" leading={<Settings2 />} asChild>
                      <Link to={`/kb?collection=${collection.slug}`}>Edit</Link>
                    </Button>
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
