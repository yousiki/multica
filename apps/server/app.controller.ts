import {
  Controller,
  Get,
  Post,
  Put,
  Delete,
  Param,
  Body,
  Inject,
} from "@nestjs/common";
import { Hub } from "@multica/core";

@Controller("api")
export class AppController {
  constructor(@Inject("HUB") private readonly hub: Hub) {}

  @Get("hub")
  getHub() {
    return {
      hubId: this.hub.hubId,
      url: this.hub.url,
      connectionState: this.hub.connectionState,
      agentCount: this.hub.listAgents().length,
    };
  }

  @Put("hub/gateway")
  updateGateway(@Body() body: { url: string }) {
    this.hub.reconnect(body.url);
    return {
      url: this.hub.url,
      connectionState: this.hub.connectionState,
    };
  }

  @Get("agents")
  listAgents() {
    return this.hub.listAgents().map((id) => {
      const agent = this.hub.getAgent(id);
      return { id, closed: agent?.closed ?? true };
    });
  }

  @Post("agents")
  createAgent(@Body() body?: { id?: string }) {
    const agent = this.hub.createAgent(body?.id);
    return { id: agent.sessionId };
  }

  @Delete("agents/:id")
  deleteAgent(@Param("id") id: string) {
    const ok = this.hub.closeAgent(id);
    return { ok };
  }

  @Get("conversations")
  listConversations() {
    return this.hub.listConversations().map((id) => {
      const conversation = this.hub.getConversation(id);
      return { id, closed: conversation?.closed ?? true };
    });
  }

  @Post("conversations")
  createConversation(@Body() body?: { id?: string; agentId?: string }) {
    const conversation = this.hub.createConversation(body?.id, { agentId: body?.agentId });
    return { id: conversation.sessionId };
  }

  @Delete("conversations/:id")
  deleteConversation(@Param("id") id: string) {
    const ok = this.hub.closeConversation(id);
    return { ok };
  }
}
