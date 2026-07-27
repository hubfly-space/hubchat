import { Button, EmptyState, Page } from "@hubchat/shared";
import { Compass } from "lucide-react";
import { Link } from "react-router-dom";

export function NotFound() {
  return (
    <Page>
      <div className="flex flex-1 items-center justify-center">
        <EmptyState
          icon={Compass}
          size="lg"
          title="This page does not exist"
          description="The link may be out of date, or the record it pointed at was deleted."
          action={
            <Button variant="primary" size="sm" asChild>
              <Link to="/overview">Back to overview</Link>
            </Button>
          }
          secondaryAction={
            <Button variant="ghost" size="sm" asChild>
              <Link to="/inbox">Go to inbox</Link>
            </Button>
          }
        />
      </div>
    </Page>
  );
}

export default NotFound;
