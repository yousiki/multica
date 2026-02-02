/** Stream Action - 流式消息传输 */

export const StreamAction = "stream" as const;

/** 流消息状态 */
export type StreamState = "delta" | "final" | "error";

/** 流消息 payload */
export interface StreamPayload {
  /** 流 ID（即 messageId），关联同一个流的所有消息 */
  streamId: string;
  /** 所属 agent ID */
  agentId: string;
  /** 流状态 */
  state: StreamState;
  /** 累计文本内容（delta/final 时） */
  content?: string;
  /** 错误信息（error 时） */
  error?: string;
}
