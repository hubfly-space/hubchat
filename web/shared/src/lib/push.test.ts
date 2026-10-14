import { beforeEach, expect, it } from "vitest";
import { hasPushSubscription, pushSubscriptionKey } from "./push";

beforeEach(() => localStorage.clear());
it("does not suppress alerts because another workspace or member enabled push", () => {
  localStorage.setItem(pushSubscriptionKey("member-a", "workspace-a"), "subscription-a");
  expect(hasPushSubscription("member-a", "workspace-a")).toBe(true);
  expect(hasPushSubscription("member-a", "workspace-b")).toBe(false);
  expect(hasPushSubscription("member-b", "workspace-a")).toBe(false);
});
