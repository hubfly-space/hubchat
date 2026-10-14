/** Push subscriptions belong to one workspace member, not the whole browser. */
export function pushSubscriptionKey(memberId: string, workspaceId: string): string {
  return `hubchat.push:${memberId}:${workspaceId}`;
}

export function hasPushSubscription(memberId: string, workspaceId: string): boolean {
  try {
    return Boolean(localStorage.getItem(pushSubscriptionKey(memberId, workspaceId)));
  } catch {
    return false;
  }
}
