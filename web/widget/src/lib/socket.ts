/**
 * The widget's realtime connection.
 *
 * A minimal, hand-rolled client for internal/realtime's wire protocol
 * (protocol.go) — no framework, since this ships inside the widget bundle.
 * It reconnects with backoff and resumes from its last known sequence on
 * every reconnect, so a dropped connection (a laptop sleeping, a flaky
 * mobile network) never loses a message: the server replays the gap instead
 * of the client re-fetching history from scratch.
 */

export type WireEvent = {
  id: string;
  workspace_id: string;
  sequence: number;
  type: string;
  entity_type?: string;
  entity_id?: string;
  occurred_at: string;
  data: unknown;
};

type Options = {
  host: string;
  publicKey: string;
  token: string;
  onEvent: (event: WireEvent) => void;
  onStatusChange: (status: "connecting" | "open" | "closed") => void;
};

const MAX_BACKOFF_MS = 15_000;

export class VisitorSocket {
  private ws: WebSocket | null = null;
  private lastSequence = 0;
  private closedByCaller = false;
  private attempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(private options: Options) {
    this.connect();
  }

  private wsURL(): string {
    const wsHost = this.options.host.replace(/^http/, "ws");
    const params = new URLSearchParams({
      key: this.options.publicKey,
      token: this.options.token,
      url: location.href,
    });
    return `${wsHost}/ws/visitor?${params.toString()}`;
  }

  private connect(): void {
    this.options.onStatusChange("connecting");
    const ws = new WebSocket(this.wsURL());
    this.ws = ws;

    ws.onopen = () => {
      this.attempt = 0;
    };

    ws.onmessage = (event: MessageEvent<string>) => {
      let frame: WireEvent;
      try {
        frame = JSON.parse(event.data) as WireEvent;
      } catch {
        return;
      }

      switch (frame.type) {
        case "hub.ready":
          // A fresh connection starts at the log's current head (see
          // Hub.Serve) — nothing to resume the first time. On a reconnect
          // that already holds a position, ask for the gap immediately.
          this.options.onStatusChange("open");
          if (this.lastSequence > 0) {
            this.send({ action: "resume", after_sequence: this.lastSequence });
          }
          return;
        case "hub.resumed":
        case "hub.pong":
        case "hub.topics":
        case "hub.error":
          return;
        default:
          if (frame.sequence > this.lastSequence) this.lastSequence = frame.sequence;
          this.options.onEvent(frame);
      }
    };

    ws.onclose = () => {
      this.options.onStatusChange("closed");
      if (this.closedByCaller) return;
      this.scheduleReconnect();
    };

    ws.onerror = () => {
      ws.close();
    };
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return;
    const delay = Math.min(MAX_BACKOFF_MS, 500 * 2 ** this.attempt) + Math.random() * 300;
    this.attempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.closedByCaller) this.connect();
    }, delay);
  }

  private send(payload: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(payload));
  }

  close(): void {
    this.closedByCaller = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.ws?.close();
  }
}
