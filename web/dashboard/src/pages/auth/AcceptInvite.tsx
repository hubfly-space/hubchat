import { Avatar, Badge, Button, Card, CardBody, Field, Input } from "@hubchat/shared";
import { Link } from "react-router-dom";
import { currentWorkspace, members } from "../../data/fixtures";

export default function AcceptInvite() {
  const inviter = members[1]!;

  return (
    <>
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-fg">
          Join {currentWorkspace.name}
        </h1>
        <p className="mt-1 text-sm text-fg-muted">
          {inviter.name} invited you to help handle support.
        </p>
      </header>

      <Card variant="raised" className="mb-6">
        <CardBody className="flex items-center gap-3">
          <Avatar
            name={currentWorkspace.name}
            seed={currentWorkspace.id}
            shape="square"
            size="lg"
            kind="company"
          />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium text-fg">{currentWorkspace.name}</p>
            <p className="text-xs text-fg-muted">{currentWorkspace.slug}.hubchat.app</p>
          </div>
          <Badge tone="accent">Agent</Badge>
        </CardBody>
      </Card>

      <form className="flex flex-col gap-4">
        <Field label="Your name" htmlFor="name">
          <Input id="name" autoComplete="name" required inputSize="lg" />
        </Field>

        <Field
          label="Password"
          htmlFor="password"
          description="At least 12 characters."
        >
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            inputSize="lg"
          />
        </Field>

        <Button type="submit" variant="primary" size="lg" fullWidth>
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
  );
}
