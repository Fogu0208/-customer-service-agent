export type Role = 'user' | 'assistant' | 'system'

export interface ChatMessage {
  id: string
  role: Role
  content: string
  intent?: string
  toolsUsed?: string[]
  compliancePassed?: boolean
  pending?: boolean
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
