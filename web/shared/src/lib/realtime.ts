/**
 * The WebSocket client.
 *
 * One socket per tab, shared by every component. Opening a connection per
 * screen would multiply idle connections by the number of open panels, and the
 * server budgets for tabs, not panels.
 *
 * What makes the UI feel live is the loop this closes: the server appends an
 * event, the socket delivers it, a handler invalidates the affected query
 * keys, and the mounted screens refetch exactly what changed. No polling, and
 * no screen has to know which other screens care.
 */

import { useEffect, useRef, useSyncExternalStore } from "react";

import type { EventEnvelope } from "../types";

export type { EventEnvelope };

export type ConnectionState = "connecting" | "open" | "closed";

type Handler = (event: EventEnvelope) => void;

/**
 * Frames the server sends that describe the connection rather than the
 * workspace. They share the event shape and are prefixed so no domain event
 * can collide with one.
 */
const HUB_READY = "hub.ready";
const HUB_RESUMED = "hub.resumed";

class RealtimeConnection {
  private socket?: WebSocket;
  private workspaceId?: string;
  private handlers = new Set<Handler>();
  private stateListeners = new Set<() => void>();
  private state: ConnectionState = "closed";

  /**
   * The last sequence applied. Persisted across reconnects so the socket can
   * ask the server for the gap rather than the caller refetching everything.
   */
  private sequence = 0;

  private attempt = 0;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  /** Set when the caller deliberately disconnected, so we do not reconnect. */
  private stopped = false;

  connect(workspaceId: string) {
    if (this.workspaceId === workspaceId && this.socket) return;

    // Switching workspaces invalidates the position: sequences are per-tenant,
    // so resuming the old one against the new workspace would ask for
    // nonsense.
    if (this.workspaceId !== workspaceId) this.sequence = 0;

    this.workspaceId = workspaceId;
    this.stopped = false;
    this.open();
  }

  disconnect() {
    this.stopped = true;
    clearTimeout(this.reconnectTimer);
    this.socket?.close();
    this.socket = undefined;
    this.setState("closed");
  }

  subscribe(handler: Handler): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  subscribeState(listener: () => void): () => void {
    this.stateListeners.add(listener);
    return () => this.stateListeners.delete(listener);
  }

  getState = (): ConnectionState => this.state;

  /** Asks the server to narrow or widen this connection's topics. */
  send(frame: Record<string, unknown>) {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(frame));
    }
  }

  private open() {
    if (!this.workspaceId || this.stopped) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws/conversations?workspace_id=${encodeURIComponent(this.workspaceId)}`;

    this.setState("connecting");

    const socket = new WebSocket(url);
    this.socket = socket;

    socket.onopen = () => {
      this.attempt = 0;
      this.setState("open");

      // Only resume when we have a position to resume from. A first connection
      // starts at the server's head and backfills through the API, because
      // replaying a workspace's entire history over a socket is not a sensible
      // way to populate an empty screen.
      if (this.sequence > 0) {
        this.send({ action: "resume", after_sequence: this.sequence });
      }
    };

    socket.onmessage = (message) => {
      let envelope: EventEnvelope;
      try {
        envelope = JSON.parse(message.data as string) as EventEnvelope;
      } catch {
        return; // a malformed frame is not worth tearing the connection down for
      }

      if (envelope.type === HUB_READY || envelope.type === HUB_RESUMED) {
        this.onControlFrame(envelope);
        return;
      }

      if (envelope.sequence > this.sequence) this.sequence = envelope.sequence;

      this.handlers.forEach((handler) => handler(envelope));
    };

    socket.onclose = () => {
      this.socket = undefined;
      this.setState("closed");
      this.scheduleReconnect();
    };

    // An error is always followed by a close, which is where reconnection is
    // handled. Doing it in both places would double the backoff.
    socket.onerror = () => {};
  }

  private onControlFrame(envelope: EventEnvelope) {
    const data = envelope.data as { sequence?: number; truncated?: boolean } | undefined;

    if (typeof data?.sequence === "number" && data.sequence > this.sequence) {
      this.sequence = data.sequence;
    }

    // The gap was larger than the server will replay. Everything on screen is
    // now suspect, so tell listeners to refetch rather than letting them
    // believe they are current.
    if (data?.truncated) {
      this.handlers.forEach((handler) =>
        handler({ ...envelope, type: "hub.desynchronised" } as EventEnvelope),
      );
    }
  }

  /**
   * Reconnects with exponential backoff and full jitter.
   *
   * The jitter matters more here than anywhere else in the client: when a
   * server restarts, every connected tab across every user is disconnected in
   * the same instant. Without jitter they would all reconnect in the same
   * instant too, and the reconnection storm would be indistinguishable from an
   * attack.
   */
  private scheduleReconnect() {
    if (this.stopped) return;

    this.attempt += 1;
    const cap = 30_000;
    const exponential = Math.min(cap, 500 * 2 ** (this.attempt - 1));
    const delay = Math.random() * exponential;

    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = setTimeout(() => this.open(), delay);
  }

  private setState(state: ConnectionState) {
    if (this.state === state) return;
    this.state = state;
    this.stateListeners.forEach((listen) => listen());
  }
}

const connection = new RealtimeConnection();

/**
 * Opens the shared socket for a workspace and reports its state.
 *
 * Safe to call from several components: the connection is a module singleton,
 * so the second caller joins the first one's socket.
 */
export function useRealtimeConnection(workspaceId: string | undefined): ConnectionState {
  useEffect(() => {
    if (!workspaceId) return;
    connection.connect(workspaceId);
    // Deliberately not disconnecting on unmount. The connection outlives any
    // one component, and tearing it down when a panel closes would reconnect
    // it a moment later when the next one mounts.
  }, [workspaceId]);

  return useSyncExternalStore(
    (listener) => connection.subscribeState(listener),
    connection.getState,
    () => "closed" as const,
  );
}

/**
 * Runs a handler for every incoming event.
 *
 * The handler is held in a ref, so an inline arrow function does not
 * resubscribe on every render.
 */
export function useRealtimeEvents(handler: Handler) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => connection.subscribe((event) => handlerRef.current(event)), []);
}

/** Closes the socket. Called on sign-out. */
export function disconnectRealtime() {
  connection.disconnect();
}

/** Sends a raw frame — subscribe/unsubscribe for narrowly-scoped surfaces. */
export function sendRealtime(frame: Record<string, unknown>) {
  connection.send(frame);
}
