import {
  api,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  useMutation,
} from "@hubchat/shared";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

type CreatedWorkspace = { id: string; name: string; slug: string };

/** Creates an additional tenant without replaying onboarding for the active workspace. */
export default function CreateWorkspace() {
  const navigate = useNavigate();
  const { switchWorkspace } = useWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);

  const create = useMutation<{ name: string; slug: string }, CreatedWorkspace>(
    (body) => api.post<CreatedWorkspace>("/workspaces", body),
    {
      onSuccess: (workspace) => {
        switchWorkspace(workspace.id);
        navigate("/overview", { replace: true });
      },
    },
  );

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void create.mutate({ name: name.trim(), slug: slug.trim() }).catch(() => {});
  };

  return (
    <Page>
      <PageHeader
        title="Create workspace"
        description="Add a separate tenant with its own customers, conversations, channels, and settings."
      />
      <PageBody width="narrow">
        <Card>
          <CardBody>
            {create.error ? (
              <Callout tone="danger" className="mb-4">
                {create.error instanceof ApiError
                  ? create.error.message
                  : "Could not create the workspace. Try again."}
              </Callout>
            ) : null}

            <form onSubmit={submit} className="flex flex-col gap-4">
              <Field label="Workspace name" htmlFor="new-workspace-name" required>
                <Input
                  id="new-workspace-name"
                  inputSize="lg"
                  required
                  autoFocus
                  value={name}
                  onChange={(event) => {
                    const value = event.target.value;
                    setName(value);
                    if (!slugTouched) setSlug(slugify(value));
                  }}
                />
              </Field>
              <Field
                label="Slug"
                htmlFor="new-workspace-slug"
                required
                description="Used in portal URLs and API scoping. Lowercase letters, numbers, and hyphens."
              >
                <Input
                  id="new-workspace-slug"
                  inputSize="lg"
                  required
                  pattern="[a-z0-9](?:[a-z0-9]|-){1,38}[a-z0-9]"
                  value={slug}
                  onChange={(event) => {
                    setSlugTouched(true);
                    setSlug(event.target.value);
                  }}
                />
              </Field>
              <div className="flex justify-end gap-2 pt-2">
                <Button type="button" variant="ghost" size="lg" onClick={() => navigate(-1)}>
                  Cancel
                </Button>
                <Button type="submit" variant="primary" size="lg" loading={create.isPending}>
                  Create workspace
                </Button>
              </div>
            </form>
          </CardBody>
        </Card>
      </PageBody>
    </Page>
  );
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}
