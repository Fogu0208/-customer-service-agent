import type { ChatRequest, ChatResponse, HealthResponse } from '../types/chat'

async function parseError(res: Response): Promise<never> {
  let detail = res.statusText
  try {
    const data = (await res.json()) as { error?: string }
    if (data.error) detail = data.error
  } catch {
    // ignore json parse errors
  }
  throw new Error(detail || `请求失败 (${res.status})`)
}

export async function checkHealth(): Promise<HealthResponse> {
  const res = await fetch('/health')
  if (!res.ok) return parseError(res)
  return res.json() as Promise<HealthResponse>
}

export async function sendChat(payload: ChatRequest): Promise<ChatResponse> {
  const res = await fetch('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) return parseError(res)
  return res.json() as Promise<ChatResponse>
}
