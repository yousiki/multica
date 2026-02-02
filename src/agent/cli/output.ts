import type { AgentEvent, AgentMessage } from "@mariozechner/pi-agent-core";
import { colors, createSpinner } from "./colors.js";
import { extractText } from "../extract-text.js";

export type AgentOutputState = {
  lastAssistantText: string;
  printedLen: number;
  streaming: boolean;
};

export type AgentOutput = {
  state: AgentOutputState;
  handleEvent: (event: AgentEvent) => void;
};

function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) + "…" : s;
}

// Exported for testing
export function toolDisplayName(name: string): string {
  const map: Record<string, string> = {
    read: "ReadFile",
    write: "WriteFile",
    edit: "EditFile",
    exec: "Exec",
    process: "Process",
    grep: "Grep",
    find: "FindFiles",
    ls: "ListDir",
    glob: "Glob",
    web_search: "WebSearch",
    web_fetch: "WebFetch",
  };
  return map[name] || name;
}

// Exported for testing
export function formatToolArgs(name: string, args: unknown): string {
  if (!args || typeof args !== "object") return "";
  const record = args as Record<string, unknown>;
  const get = (key: string) => (record[key] !== undefined ? String(record[key]) : "");
  switch (name) {
    case "read":
      return get("path") || get("file");
    case "write":
      return get("path") || get("file");
    case "edit":
      return get("path") || get("file");
    case "grep":
      return [get("pattern"), get("path") || get("directory")].filter(Boolean).join(" ");
    case "find":
      return [get("glob") || get("pattern"), get("path") || get("directory")].filter(Boolean).join(" ");
    case "ls":
      return get("path") || get("directory");
    case "exec":
      return get("command");
    case "process":
      return [get("action"), get("id")].filter(Boolean).join(" ");
    case "glob":
      return [get("pattern"), get("cwd")].filter(Boolean).join(" in ");
    case "web_search":
      return truncate(get("query"), 50);
    case "web_fetch": {
      const url = get("url");
      try {
        const parsed = new URL(url);
        return parsed.hostname + (parsed.pathname !== "/" ? truncate(parsed.pathname, 30) : "");
      } catch {
        return truncate(url, 50);
      }
    }
    default:
      return "";
  }
}

function formatToolLine(name: string, args: unknown, result?: unknown): string {
  const title = colors.toolName(toolDisplayName(name));
  const argText = formatToolArgs(name, args);
  const resultSummary = formatResultSummary(name, result);
  const bullet = colors.toolBullet("•");

  let line = `${bullet} ${title}`;
  if (argText) {
    line += ` ${colors.toolArgs(`(${argText})`)}`;
  }
  if (resultSummary) {
    line += ` ${colors.toolArrow("→")} ${colors.toolArgs(resultSummary)}`;
  }
  return line;
}

// Exported for testing
export function extractResultDetails(result: unknown): Record<string, unknown> | null {
  if (!result || typeof result !== "object") return null;

  // Try to extract from AgentMessage content array (JSON result)
  const msg = result as { content?: Array<{ type: string; text?: string }> };
  if (Array.isArray(msg.content)) {
    for (const c of msg.content) {
      if (c.type === "text" && c.text) {
        try {
          return JSON.parse(c.text) as Record<string, unknown>;
        } catch {
          continue;
        }
      }
    }
  }

  // Try direct object access
  return result as Record<string, unknown>;
}

// Exported for testing
export function formatResultSummary(name: string, result: unknown): string {
  const details = extractResultDetails(result);
  if (!details) return "";

  switch (name) {
    case "glob": {
      const count = details.count ?? (Array.isArray(details.files) ? details.files.length : 0);
      const truncated = details.truncated ? "+" : "";
      return `${count}${truncated} files`;
    }
    case "web_search": {
      if (details.error) return `error: ${details.message || details.error}`;
      if (details.content) {
        // Perplexity result
        const citations = Array.isArray(details.citations) ? details.citations.length : 0;
        return `${citations} citations`;
      }
      // Brave result
      const count = details.count ?? (Array.isArray(details.results) ? details.results.length : 0);
      return `${count} results`;
    }
    case "web_fetch": {
      if (details.error) return `error: ${details.message || details.error}`;
      const parts: string[] = [];
      if (details.title) {
        parts.push(`"${truncate(String(details.title), 30)}"`);
      }
      if (typeof details.length === "number") {
        const kb = (details.length / 1024).toFixed(1);
        parts.push(`${kb}KB`);
      }
      if (details.cached) {
        parts.push("cached");
      }
      return parts.join(", ");
    }
    case "grep": {
      // Try to count matches from result text
      const text = extractText(result as AgentMessage | undefined);
      if (text.includes("No matches found")) return "no matches";
      const lines = text.split("\n").filter((l) => l.trim()).length;
      if (lines > 0) return `${lines} matches`;
      return "";
    }
    default:
      return "";
  }
}

export function createAgentOutput(params: {
  stdout: NodeJS.WritableStream;
  stderr: NodeJS.WritableStream;
}): AgentOutput {
  const state: AgentOutputState = {
    lastAssistantText: "",
    printedLen: 0,
    streaming: false,
  };

  // Create spinner for thinking indicator
  const spinner = createSpinner({ stream: params.stderr });
  let pendingToolName = "";
  let pendingToolArgs: unknown = null;

  const handleEvent = (event: AgentEvent) => {
    switch (event.type) {
      case "message_start": {
        const msg = event.message;
        if (msg.role === "assistant") {
          // Stop any running spinner when assistant starts responding
          if (spinner.isSpinning()) {
            spinner.stop();
          }
          state.streaming = true;
          state.printedLen = 0;
          const text = extractText(msg);
          if (text.length > 0) {
            params.stdout.write(text);
            state.printedLen = text.length;
          }
        }
        break;
      }
      case "message_update": {
        const msg = event.message;
        if (msg.role === "assistant") {
          const text = extractText(msg);
          if (text.length > state.printedLen) {
            params.stdout.write(text.slice(state.printedLen));
            state.printedLen = text.length;
          }
        }
        break;
      }
      case "message_end": {
        const msg = event.message;
        if (msg.role === "assistant") {
          const text = extractText(msg);
          if (text.length > state.printedLen) {
            params.stdout.write(text.slice(state.printedLen));
            state.printedLen = text.length;
          }
          if (state.streaming) params.stdout.write("\n");
          state.streaming = false;
          state.lastAssistantText = text;
        }
        break;
      }
      case "tool_execution_start": {
        pendingToolName = event.toolName;
        pendingToolArgs = event.args;
        const title = colors.toolName(toolDisplayName(event.toolName));
        const argText = formatToolArgs(event.toolName, event.args);
        const displayText = argText ? `${title} ${colors.toolArgs(`(${argText})`)}` : title;
        spinner.start(displayText);
        break;
      }
      case "tool_execution_update": {
        // Show real-time output updates (e.g., from exec tool)
        const updateText = extractText(event.partialResult);
        if (updateText && pendingToolName) {
          const title = colors.toolName(toolDisplayName(pendingToolName));
          const preview = colors.toolArgs(updateText.slice(-50).replace(/\n/g, " "));
          spinner.update(`${title} ${colors.toolArrow("→")} ${preview}`);
        }
        break;
      }
      case "tool_execution_end": {
        // Stop spinner and show final result with summary
        if (event.isError) {
          const errorText = extractText(event.result) || "Tool failed";
          const bullet = colors.toolError("✗");
          const title = colors.toolName(toolDisplayName(event.toolName));
          spinner.stop(`${bullet} ${title}: ${colors.toolError(errorText)}`);
        } else {
          spinner.stop(formatToolLine(event.toolName, pendingToolArgs, event.result));
        }
        pendingToolName = "";
        pendingToolArgs = null;
        break;
      }
      default:
        break;
    }
  };

  return { state, handleEvent };
}
