import { EventEmitter } from "node:events";
import { appendFileSync } from "node:fs";
import type { BridgeMessage } from "./types";
import type { ControlClientMessage, ControlServerMessage, DaemonStatus } from "./control-protocol";

interface DaemonClientEvents {
  codexMessage: [BridgeMessage];
  disconnect: [{ code: number; reason: string; uptimeMs: number }];
  status: [DaemonStatus];
}

export interface DaemonClientOptions {
  logFile?: string;
  verbose?: boolean;
}

let nextSocketId = 0;

export class DaemonClient extends EventEmitter<DaemonClientEvents> {
  private ws: WebSocket | null = null;
  private wsId: number = 0; // Track socket identity for debugging
  private nextRequestId = 1;
  private connectedAt: number | null = null;
  private readonly logFile?: string;
  private readonly verbose: boolean;
  private pendingReplies = new Map<
    string,
    {
      resolve: (value: { success: boolean; error?: string }) => void;
      timer: ReturnType<typeof setTimeout>;
    }
  >();

  constructor(private readonly url: string, options: DaemonClientOptions = {}) {
    super();
    this.logFile = options.logFile;
    this.verbose = options.verbose ?? false;
  }

  async connect() {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.log(`connect() skipped — ws#${this.wsId} already OPEN`);
      return;
    }

    // Close any lingering socket in non-OPEN state to avoid orphans
    if (this.ws) {
      const state = this.ws.readyState;
      this.log(`connect() closing lingering ws#${this.wsId} (readyState=${state})`);
      try { this.ws.close(); } catch (err: any) {
        this.log(`connect() error closing lingering ws#${this.wsId}: ${err?.message ?? "unknown"}`);
      }
      this.ws = null;
    }

    const socketId = ++nextSocketId;
    const attemptStart = Date.now();

    await new Promise<void>((resolve, reject) => {
      let ws: WebSocket;
      try {
        ws = new WebSocket(this.url);
      } catch (err: any) {
        this.log(`ws#${socketId} construction failed: ${err?.message ?? "unknown"} (url=${this.url})`);
        reject(err);
        return;
      }
      let settled = false;

      ws.onopen = () => {
        settled = true;
        this.ws = ws;
        this.wsId = socketId;
        this.connectedAt = Date.now();
        this.attachSocketHandlers(ws, socketId);
        this.log(`ws#${socketId} opened and attached (connect took ${Date.now() - attemptStart}ms)`);
        resolve();
      };

      ws.onerror = (event: Event) => {
        if (settled) return;
        settled = true;
        const errDetail = (event as ErrorEvent).message ?? "unknown";
        this.log(`ws#${socketId} connect onerror: ${errDetail} (url=${this.url}, elapsed=${Date.now() - attemptStart}ms)`);
        reject(new Error(`Failed to connect to AgentBridge daemon at ${this.url}: ${errDetail}`));
      };

      ws.onclose = (event: CloseEvent) => {
        if (settled) return;
        settled = true;
        this.log(
          `ws#${socketId} closed during startup (code=${event.code}, reason=${event.reason || "none"}, clean=${event.wasClean}, elapsed=${Date.now() - attemptStart}ms)`,
        );
        reject(new Error(`AgentBridge daemon closed the connection during startup (${this.url}, code=${event.code})`));
      };
    });
  }

  attachClaude() {
    this.send({ type: "claude_connect" });
  }

  async disconnect() {
    if (!this.ws) return;

    try {
      this.send({ type: "claude_disconnect" });
    } catch {}

    try {
      this.ws.close();
    } catch {}

    this.ws = null;
    this.rejectPendingReplies("Daemon connection closed");
  }

  async sendReply(message: BridgeMessage, requireReply?: boolean): Promise<{ success: boolean; error?: string }> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return { success: false, error: "AgentBridge daemon is not connected." };
    }

    const requestId = `reply_${Date.now()}_${this.nextRequestId++}`;
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        this.pendingReplies.delete(requestId);
        resolve({ success: false, error: "Timed out waiting for AgentBridge daemon reply." });
      }, 15000);

      this.pendingReplies.set(requestId, { resolve, timer });
      this.send({
        type: "claude_to_codex",
        requestId,
        message,
        ...(requireReply ? { requireReply: true } : {}),
      });
    });
  }

  private attachSocketHandlers(ws: WebSocket, socketId: number) {
    ws.onmessage = (event) => {
      const raw = typeof event.data === "string" ? event.data : event.data.toString();

      let message: ControlServerMessage;
      try {
        message = JSON.parse(raw);
      } catch {
        return;
      }

      switch (message.type) {
        case "codex_to_claude":
          this.emit("codexMessage", message.message);
          return;
        case "claude_to_codex_result": {
          const pending = this.pendingReplies.get(message.requestId);
          if (!pending) return;
          clearTimeout(pending.timer);
          this.pendingReplies.delete(message.requestId);
          pending.resolve({ success: message.success, error: message.error });
          return;
        }
        case "status":
          this.emit("status", message.status);
          return;
      }
    };

    ws.onclose = (event) => {
      const isCurrent = this.ws === ws;
      const uptimeMs = this.connectedAt ? Date.now() - this.connectedAt : 0;
      const uptime = this.connectedAt ? `${(uptimeMs / 1000).toFixed(1)}s` : "n/a";
      this.log(
        `ws#${socketId} onclose (code=${event.code}, reason=${event.reason || "none"}, clean=${event.wasClean}, uptime=${uptime}, isCurrent=${isCurrent}, currentWsId=${this.wsId}, pendingReplies=${this.pendingReplies.size})`,
      );
      if (isCurrent) {
        this.ws = null;
        this.connectedAt = null;
        this.rejectPendingReplies("AgentBridge daemon disconnected.");
        this.emit("disconnect", { code: event.code, reason: event.reason || "", uptimeMs });
      }
      // If this.ws !== ws, this socket was replaced by a newer connection —
      // don't emit "disconnect" or it will trigger a reconnect loop.
    };

    ws.onerror = (event: Event) => {
      // The close handler is the single place that tears down pending state,
      // but capture the error detail for root-cause analysis.
      const errDetail = (event as ErrorEvent).message ?? "unknown";
      this.log(`ws#${socketId} onerror: ${errDetail} (readyState=${ws.readyState}, isCurrent=${this.ws === ws})`);
    };
  }

  private rejectPendingReplies(error: string) {
    for (const [requestId, pending] of this.pendingReplies.entries()) {
      clearTimeout(pending.timer);
      pending.resolve({ success: false, error });
      this.pendingReplies.delete(requestId);
    }
  }

  private send(message: ControlClientMessage) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error("AgentBridge daemon socket is not open.");
    }

    this.ws.send(JSON.stringify(message));
  }

  private log(msg: string) {
    const line = `[${new Date().toISOString()}] [DaemonClient] ${msg}\n`;
    process.stderr.write(line);
    if (this.logFile) {
      try { appendFileSync(this.logFile, line); } catch {}
    }
  }
}
