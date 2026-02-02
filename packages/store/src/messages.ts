import { create } from "zustand"
import { v7 as uuidv7 } from "uuid"

export interface Message {
  id: string
  role: "user" | "assistant"
  content: string
  agentId: string
}

interface MessagesState {
  messages: Message[]
  streamingIds: Set<string>
}

interface MessagesActions {
  addUserMessage: (content: string, agentId: string) => void
  addAssistantMessage: (content: string, agentId: string) => void
  updateMessage: (id: string, content: string) => void
  loadMessages: (agentId: string, msgs: Message[]) => void
  clearMessages: (agentId?: string) => void
  startStream: (streamId: string, agentId: string) => void
  appendStream: (streamId: string, content: string) => void
  endStream: (streamId: string, content: string) => void
}

export type MessagesStore = MessagesState & MessagesActions

export const useMessagesStore = create<MessagesStore>()((set, get) => ({
  messages: [],
  streamingIds: new Set<string>(),

  addUserMessage: (content, agentId) => {
    set((s) => ({
      messages: [...s.messages, { id: uuidv7(), role: "user", content, agentId }],
    }))
  },

  addAssistantMessage: (content, agentId) => {
    set((s) => ({
      messages: [...s.messages, { id: uuidv7(), role: "assistant", content, agentId }],
    }))
  },

  updateMessage: (id, content) => {
    set((s) => ({
      messages: s.messages.map((m) => (m.id === id ? { ...m, content } : m)),
    }))
  },

  loadMessages: (agentId, msgs) => {
    set((s) => ({
      messages: [...s.messages.filter((m) => m.agentId !== agentId), ...msgs],
    }))
  },

  clearMessages: (agentId?) => {
    set((s) => ({
      messages: agentId ? s.messages.filter((m) => m.agentId !== agentId) : [],
    }))
  },

  startStream: (streamId, agentId) => {
    set((s) => {
      const ids = new Set(s.streamingIds)
      ids.add(streamId)
      return {
        messages: [...s.messages, { id: streamId, role: "assistant" as const, content: "", agentId }],
        streamingIds: ids,
      }
    })
  },

  appendStream: (streamId, content) => {
    set((s) => ({
      messages: s.messages.map((m) => (m.id === streamId ? { ...m, content } : m)),
    }))
  },

  endStream: (streamId, content) => {
    set((s) => {
      const ids = new Set(s.streamingIds)
      ids.delete(streamId)
      return {
        messages: s.messages.map((m) => (m.id === streamId ? { ...m, content } : m)),
        streamingIds: ids,
      }
    })
  },
}))
