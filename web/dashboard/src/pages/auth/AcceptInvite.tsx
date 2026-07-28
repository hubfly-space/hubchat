import {
  api,
  ApiError,
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  clearQueryCache,
  Field,
  Input,
  QueryBoundary,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { Link, useNavigate, useParams } from "react-router-dom";

type InviteDetails = {
  workspace_name: string;
  email: string;
  role: string;
  expires_at: string;
};

type RedeemResult = { workspace_id: string; workspace_name: string; workspace_slug: string };

export default function AcceptInvite() {
  const navigate = useNavigate();
  const { token } = useParams<{ token: string }>();

  const invite = useQuery<InviteDetails>(
    token ? ["invite", token] : null,
    (signal) => api.get<InviteDetails>(`/invites/lookup/${token}`, { signal }),
    { staleTime: Infinity },
  );

  const redeem = useMutation<{ token: string; name: string; password: string }, RedeemResult>(
    (body) => api.post<RedeemResult>("/invites/redeem", body),
    {
      onSuccess: () => {
        clearQueryCache();
        navigate("/overview", { replace: true });
      },
    },
  );

  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    void redeem
      .mutate({
        token: token ?? "",
        name: String(form.get("name") ?? ""),
        password: String(form.get("password") ?? ""),
      })
      .catch(() => {});
  };

  return (
    <QueryBoundary query={invite}>
      {(details) => (
        <>
          <header className="mb-6">
            <h1 className="text-xl font-semibold tracking-tight text-fg">
              Join {details.workspace_name}
            </h1>
            <p className="mt-1 text-sm text-fg-muted">
              You were invited to help handle support at {details.email}.
            </p>
          </header>

          <Card variant="raised" className="mb-6">
            <CardBody className="flex items-center gap-3">
              <Avatar
                name={details.workspace_name}
                seed={details.workspace_name}
                shape="square"
                size="lg"
                kind="company"
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-fg">{details.workspace_name}</p>
                <p className="text-xs text-fg-muted">{details.email}</p>
              </div>
              <Badge tone="accent">{details.role}</Badge>
            </CardBody>
          </Card>

          {redeem.error && (
            <Callout tone="danger" className="mb-4">
              {redeem.error instanceof ApiError
                ? redeem.error.message
                : "Could not reach the server. Check your connection and try again."}
            </Callout>
          )}

          <form onSubmit={submit} className="flex flex-col gap-4">
            <Field label="Your name" htmlFor="name">
              <Input id="name" name="name" autoComplete="name" required inputSize="lg" />
            </Field>

            <Field label="Password" htmlFor="password" description="At least 12 characters.">
              <Input
                id="password"
                name="password"
                type="password"
                autoComplete="new-password"
                required
                minLength={12}
                inputSize="lg"
              />
            </Field>

            <Button type="submit" variant="primary" size="lg" fullWidth loading={redeem.isPending}>
              Accept invitation
            </Button>
          </form>

          <p className="mt-6 text-center text-xs text-fg-muted">
            Already have a Hubchat account?{" "}
            <Link to="/login" className="text-accent-text hover:underline">
              Sign in to accept
            </Link>
          </p>
        </>
      )}
    </QueryBoundary>
  );
}
