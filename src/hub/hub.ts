import {
  GatewayClient,
  type ConnectionState,
  RequestAction,
  ResponseAction,
  StreamAction,
  type RequestPayload,
  type ResponseSuccessPayload,
  type ResponseErrorPayload,
} from "@multica/sdk";
import { AsyncAgent } from "../agent/async-agent.js";
import { getHubId } from "./hub-identity.js";
import { loadAgentRecords, addAgentRecord, removeAgentRecord } from "./agent-store.js";
import { RpcDispatcher, RpcError } from "./rpc/dispatcher.js";
import { createGetAgentMessagesHandler } from "./rpc/handlers/get-agent-messages.js";
import { createGetHubInfoHandler } from "./rpc/handlers/get-hub-info.js";
import { createListAgentsHandler } from "./rpc/handlers/list-agents.js";
import { createCreateAgentHandler } from "./rpc/handlers/create-agent.js";
import { createDeleteAgentHandler } from "./rpc/handlers/delete-agent.js";
import { createUpdateGatewayHandler } from "./rpc/handlers/update-gateway.js";

export class Hub {
  private readonly agents = new Map<string, AsyncAgent>();
  private readonly agentSenders = new Map<string, string>();
  private readonly rpc: RpcDispatcher;
  private client: GatewayClient;
  url: string;
  readonly path: string;
  readonly hubId: string;

  /** Current Gateway connection state */
  get connectionState(): ConnectionState {
    return this.client.state;
  }

  constructor(url: string, path?: string) {
    this.url = url;
    this.path = path ?? "/ws";
    this.hubId = getHubId();

    this.rpc = new RpcDispatcher();
    this.rpc.register("getAgentMessages", createGetAgentMessagesHandler());
    this.rpc.register("getHubInfo", createGetHubInfoHandler(this));
    this.rpc.register("listAgents", createListAgentsHandler(this));
    this.rpc.register("createAgent", createCreateAgentHandler(this));
    this.rpc.register("deleteAgent", createDeleteAgentHandler(this));
    this.rpc.register("updateGateway", createUpdateGatewayHandler(this));

    this.client = this.createClient(this.url);
    this.client.connect();
    this.restoreAgents();
  }

  /** Restore agents from persistent storage */
  private restoreAgents(): void {
    const records = loadAgentRecords();
    for (const record of records) {
      this.createAgent(record.id, { persist: false });
    }
    if (records.length > 0) {
      console.log(`[Hub] Restored ${records.length} agent(s)`);
    }
  }

  private createClient(url: string): GatewayClient {
    const client = new GatewayClient({
      url,
      path: this.path,
      deviceId: this.hubId,
      deviceType: "hub",
      autoReconnect: true,
      reconnectDelay: 1000,
    });

    client.onStateChange((state) => {
      console.log(`[Hub] Connection state: ${state}`);
    });

    client.onRegistered((deviceId) => {
      console.log(`[Hub] Registered as: ${deviceId}`);
    });

    client.onError((err) => {
      console.error(`[Hub] Connection error:`, err.message);
    });

    client.onMessage((msg) => {
      console.log(`[Hub] Received message: id=${msg.id} from=${msg.from} to=${msg.to} action=${msg.action} payload=${JSON.stringify(msg.payload)}`);

      // RPC request
      if (msg.action === RequestAction) {
        const payload = msg.payload as RequestPayload;
        void this.handleRpc(msg.from, payload);
        return;
      }

      // Regular chat message
      const payload = msg.payload as { agentId?: string; content?: string } | undefined;
      const agentId = payload?.agentId;
      const content = payload?.content;
      if (!agentId || !content) {
        console.warn(`[Hub] Invalid payload, missing agentId or content`);
        return;
      }
      const agent = this.agents.get(agentId);
      if (agent && !agent.closed) {
        this.agentSenders.set(agentId, msg.from);
        agent.write(content);
      } else {
        console.warn(`[Hub] Agent not found or closed: ${agentId}`);
      }
    });

    client.onSendError((err) => {
      console.error(`[Hub] Send error: messageId=${err.messageId} code=${err.code} error=${err.error}`);
    });

    return client;
  }

  /** 重连到新的 Gateway 地址 */
  reconnect(url: string): void {
    console.log(`[Hub] Reconnecting to ${url}`);
    this.client.disconnect();
    this.url = url;
    this.client = this.createClient(url);
    this.client.connect();
  }

  /** Create new Agent, or rebuild with existing ID */
  createAgent(id?: string, options?: { persist?: boolean }): AsyncAgent {
    if (id) {
      const existing = this.agents.get(id);
      if (existing && !existing.closed) {
        return existing;
      }
    }

    const agent = new AsyncAgent({ sessionId: id });
    this.agents.set(agent.sessionId, agent);

    // Persist to agent store (skip during restore to avoid duplicates)
    if (options?.persist !== false) {
      addAgentRecord({ id: agent.sessionId, createdAt: Date.now() });
    }

    // Forward streaming events to the requesting client
    agent.onStream((payload) => {
      const targetDeviceId = this.agentSenders.get(agent.sessionId);
      if (targetDeviceId) {
        this.client.send(targetDeviceId, StreamAction, payload);
      }
    });

    // Internally consume messages produced by agent (fallback for non-stream scenarios)
    void this.consumeAgent(agent);

    console.log(`Agent created: ${agent.sessionId}`);
    return agent;
  }

  /** Internally read agent output and send via Gateway */
  private async consumeAgent(agent: AsyncAgent): Promise<void> {
    for await (const msg of agent.read()) {
      console.log(`[${agent.sessionId}] ${msg.content}`);
      const targetDeviceId = this.agentSenders.get(agent.sessionId);
      if (targetDeviceId) {
        this.client.send(targetDeviceId, "message", {
          agentId: agent.sessionId,
          content: msg.content,
        });
      }
    }
  }

  /** Handle RPC request and send response back via Gateway */
  private async handleRpc(from: string, request: RequestPayload): Promise<void> {
    const { requestId, method } = request;
    try {
      const result = await this.rpc.dispatch(method, request.params);
      this.client.send<ResponseSuccessPayload>(from, ResponseAction, {
        requestId,
        ok: true,
        payload: result,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      const code = err instanceof RpcError ? err.code : "RPC_ERROR";
      console.error(`[Hub] RPC error: method=${method} code=${code} error=${message}`);
      this.client.send<ResponseErrorPayload>(from, ResponseAction, {
        requestId,
        ok: false,
        error: { code, message },
      });
    }
  }

  getAgent(id: string): AsyncAgent | undefined {
    return this.agents.get(id);
  }

  listAgents(): string[] {
    return Array.from(this.agents.entries())
      .filter(([, a]) => !a.closed)
      .map(([id]) => id);
  }

  closeAgent(id: string): boolean {
    const agent = this.agents.get(id);
    if (!agent) return false;
    agent.close();
    this.agents.delete(id);
    this.agentSenders.delete(id);
    removeAgentRecord(id);
    return true;
  }

  shutdown(): void {
    for (const [id, agent] of this.agents) {
      agent.close();
      this.agents.delete(id);
    }
    this.client.disconnect();
    console.log("Hub shut down");
  }
}
