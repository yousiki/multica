import type { AgentOptions } from "./types.js";
import { getModel } from "@mariozechner/pi-ai";
import { createCodingTools } from "@mariozechner/pi-coding-agent";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import { createExecTool } from "./tools/exec.js";
import { createProcessTool } from "./tools/process.js";

export function resolveModel(options: AgentOptions) {
  if (options.provider && options.model) {
    return getModel(options.provider, options.model);
  }
  return getModel("kimi-coding", "kimi-k2-thinking");
}

export function resolveTools(options: AgentOptions) {
  const cwd = options.cwd ?? process.cwd();
  const baseTools = createCodingTools(cwd).filter((tool) => tool.name !== "bash");
  const execTool = createExecTool(cwd);
  const processTool = createProcessTool(cwd);
  return [...baseTools, execTool as AgentTool<any>, processTool as AgentTool<any>];
}
