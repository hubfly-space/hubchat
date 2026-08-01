/** TypeScript declarations for the public Hubchat widget loader. */

export type HubchatAttributes = Record<string, unknown>;

export interface HubchatBootOptions {
  key: string;
}

export interface HubchatIdentifyOptions {
  name?: string;
  email?: string;
  external_id?: string;
  /** A signed identity token issued by the workspace's backend. */
  signed_token?: string;
  /** `token` is retained as a compatibility alias for older integrations. */
  token?: string;
  attributes?: HubchatAttributes;
}

export interface HubchatTrackOptions {
  type: string;
  payload?: HubchatAttributes;
}

export type HubchatLifecycleEvent =
  | "ready"
  | "open"
  | "close"
  | "message:received"
  | "conversation:started"
  | "unread:changed";

export type HubchatLifecyclePayload =
  | undefined
  | { count: number }
  | { id: string; body: string }
  | { id: string };

export interface HubchatSDK {
  (method: "boot", options: HubchatBootOptions): void;
  (method: "show" | "open" | "hide" | "close" | "toggle" | "reset"): void;
  (method: "identify", options: HubchatIdentifyOptions): void;
  (method: "context", context: HubchatAttributes): void;
  (method: "update", options: { attributes: HubchatAttributes }): void;
  (method: "track", options: HubchatTrackOptions): void;
  (method: "startConversation", options?: { message?: string }): void;
  (method: "openArticle", options: { slug: string }): void;
  (method: "openForm" | "openTicketForm" | "openFeedback" | "openFeedbackForm", options?: { slug?: string }): void;
  (method: "on", options: { event: HubchatLifecycleEvent; handler: (payload: HubchatLifecyclePayload) => void }): void;
  /** Commands queued by the loader before boot or lazy app loading completes. */
  q: Array<[string, unknown?]>;
}

declare global {
  interface Window {
    Hubchat: HubchatSDK;
  }
}
