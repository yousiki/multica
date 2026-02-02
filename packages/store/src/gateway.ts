import { create } from "zustand"
import { GatewayClient, StreamAction, type ConnectionState, type DeviceInfo, type SendErrorResponse, type StreamPayload } from "@multica/sdk"
import { useMessagesStore } from "./messages"

const DEFAULT_GATEWAY_URL = "http://localhost:3000"

interface GatewayState {
  gatewayUrl: string
  connectionState: ConnectionState
  hubId: string | null
  hubs: DeviceInfo[]
  lastError: SendErrorResponse | null
}

interface GatewayActions {
  setGatewayUrl: (url: string) => void
  connect: (deviceId: string) => void
  disconnect: () => void
  setHubId: (hubId: string) => void
  listDevices: () => Promise<DeviceInfo[]>
  send: (to: string, action: string, payload: unknown) => void
  request: <T = unknown>(method: string, params?: unknown) => Promise<T>
}

export type GatewayStore = GatewayState & GatewayActions

let client: GatewayClient | null = null

export const useGatewayStore = create<GatewayStore>()((set, get) => ({
  gatewayUrl: DEFAULT_GATEWAY_URL,
  connectionState: "disconnected",
  hubId: null,
  hubs: [],
  lastError: null,

  setGatewayUrl: (url) => set({ gatewayUrl: url }),

  connect: (deviceId) => {
    if (client) return

    client = new GatewayClient({
      url: get().gatewayUrl,
      deviceId,
      deviceType: "client",
    })
      .onStateChange((connectionState) => set({ connectionState }))
      .onMessage((msg) => {
        // Handle streaming messages
        if (msg.action === StreamAction) {
          const payload = msg.payload as StreamPayload
          const store = useMessagesStore.getState()
          switch (payload.state) {
            case "delta": {
              const exists = store.messages.some((m) => m.id === payload.streamId)
              if (!exists) {
                store.startStream(payload.streamId, payload.agentId)
              }
              if (payload.content) {
                store.appendStream(payload.streamId, payload.content)
              }
              break
            }
            case "final":
              store.endStream(payload.streamId, payload.content ?? "")
              break
            case "error":
              store.endStream(payload.streamId, `[error] ${payload.error}`)
              break
          }
          return
        }

        // Fallback: complete message handling
        const payload = msg.payload as { agentId?: string; content?: string }
        if (payload?.agentId && payload?.content) {
          useMessagesStore.getState().addAssistantMessage(payload.content, payload.agentId)
        }
      })
      .onSendError((error) => set({ lastError: error }))

    client.connect()
  },

  disconnect: () => {
    if (client) {
      client.disconnect()
      client = null
    }
    set({ connectionState: "disconnected", hubId: null, hubs: [] })
  },

  setHubId: (hubId) => set({ hubId }),

  listDevices: async () => {
    if (!client?.isRegistered) return []
    const devices = await client.listDevices()
    const hubs = devices.filter((d) => d.deviceType === "hub")
    set({ hubs })
    return devices
  },

  send: (to, action, payload) => {
    if (!client?.isRegistered) return
    client.send(to, action, payload)
  },

  request: <T = unknown>(method: string, params?: unknown): Promise<T> => {
    const { hubId } = get()
    if (!client?.isRegistered || !hubId) {
      return Promise.reject(new Error("Not connected"))
    }
    return client.request<T>(hubId, method, params)
  },
}))
