import { v7 as uuidv7 } from "uuid";
import { Agent } from "./runner.js";
import { Channel } from "./channel.js";
import { extractText } from "./extract-text.js";
import type { AgentOptions, Message } from "./types.js";
import type { StreamPayload } from "@multica/sdk";

const devNull = { write: () => true } as NodeJS.WritableStream;

export class AsyncAgent {
  private readonly agent: Agent;
  private readonly channel = new Channel<Message>();
  private _closed = false;
  private queue: Promise<void> = Promise.resolve();
  private streamCallback?: (payload: StreamPayload) => void;
  readonly sessionId: string;

  constructor(options?: AgentOptions) {
    this.agent = new Agent({
      ...options,
      logger: { stdout: devNull, stderr: devNull },
    });
    this.sessionId = this.agent.sessionId;
    this.setupStreamEvents();
  }

  get closed(): boolean {
    return this._closed;
  }

  /** Register callback for streaming events */
  onStream(cb: (payload: StreamPayload) => void): void {
    this.streamCallback = cb;
  }

  /** Write message to agent (non-blocking, serialized queue) */
  write(content: string): void {
    if (this._closed) throw new Error("Agent is closed");

    this.queue = this.queue
      .then(async () => {
        if (this._closed) return;
        const result = await this.agent.run(content);
        // Only send final message via channel if no stream callback
        // (stream callback already sent the final content)
        if (!this.streamCallback) {
          if (result.text) {
            this.channel.send({ id: uuidv7(), content: result.text });
          }
          if (result.error) {
            this.channel.send({ id: uuidv7(), content: `[error] ${result.error}` });
          }
        }
      })
      .catch((err) => {
        const message = err instanceof Error ? err.message : String(err);
        this.channel.send({ id: uuidv7(), content: `[error] ${message}` });
      });
  }

  /** Continuously read message stream */
  read(): AsyncIterable<Message> {
    return this.channel;
  }

  /** Close agent, stop all reads */
  close(): void {
    if (this._closed) return;
    this._closed = true;
    this.channel.close();
  }

  private setupStreamEvents(): void {
    let currentStreamId: string | null = null;

    this.agent.subscribe((event) => {
      if (!this.streamCallback) return;

      switch (event.type) {
        case "message_start": {
          if (event.message.role === "assistant") {
            currentStreamId = uuidv7();
            this.streamCallback({
              streamId: currentStreamId,
              agentId: this.sessionId,
              state: "delta",
              content: extractText(event.message),
            });
          }
          break;
        }
        case "message_update": {
          if (event.message.role === "assistant" && currentStreamId) {
            this.streamCallback({
              streamId: currentStreamId,
              agentId: this.sessionId,
              state: "delta",
              content: extractText(event.message),
            });
          }
          break;
        }
        case "message_end": {
          if (event.message.role === "assistant" && currentStreamId) {
            this.streamCallback({
              streamId: currentStreamId,
              agentId: this.sessionId,
              state: "final",
              content: extractText(event.message),
            });
            currentStreamId = null;
          }
          break;
        }
      }
    });
  }
}
