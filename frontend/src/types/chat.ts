export type Role = 'user' | 'assistant' | 'system'

export interface ChatMessage {
  id: string
  role: Role
  content: string
  intent?: string
  toolsUsed?: string[]
  compliancePassed?: boolean
  pending?: boolean
  streaming?: boolean
  node?: string
  firstTokenMs?: number | null
  totalMs?: number
}

export interface ChatRequest {
  message: string
  user_id?: string
  session_id?: string
}

export interface ChatResponse {
  response: string
  session_id: string
  intent: string
  tools_used?: string[]
  compliance_passed: boolean
}

export interface HealthResponse {
  status: string
  version: string
}

export interface StreamDone {
  session_id: string
  intent: string
  tools_used?: string[]
  compliance_passed: boolean
  violations?: string[] | null
  first_token_ms: number | null
  total_ms: number
}

/** 后端 SSE 事件的回调集合。delta 仅用于抢首字，replace/done 才是权威结果。 */
export interface StreamHandlers {
  onMeta?: (sessionId: string) => void
  onNode?: (node: string) => void
  onDelta?: (text: string) => void
  onReplace?: (text: string) => void
  onDone?: (info: StreamDone) => void
}
