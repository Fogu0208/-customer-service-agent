import type {
  ChatRequest,
  ChatResponse,
  HealthResponse,
  StreamDone,
  StreamHandlers,
} from '../types/chat'

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

/**
 * 订阅 /api/chat/stream。EventSource 只支持 GET，
 * 因此这里手动按 SSE 帧格式解析 fetch 返回的响应流。
 */
export async function streamChat(
  payload: ChatRequest,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api/chat/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  })
  if (!res.ok) return parseError(res)
  if (!res.body) throw new Error('当前环境不支持流式响应')

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break

    buffer += decoder.decode(value, { stream: true })

    // SSE 以空行分帧；最后一段可能还不完整，留在缓冲区等下一个分片
    const frames = buffer.split('\n\n')
    buffer = frames.pop() ?? ''
    for (const frame of frames) dispatchFrame(frame, handlers)
  }

  if (buffer.trim()) dispatchFrame(buffer, handlers)
}

function dispatchFrame(frame: string, handlers: StreamHandlers) {
  let event = 'message'
  const dataLines: string[] = []

  for (const line of frame.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).trim())
  }
  if (dataLines.length === 0) return

  let data: Record<string, unknown>
  try {
    data = JSON.parse(dataLines.join('\n')) as Record<string, unknown>
  } catch {
    return
  }

  switch (event) {
    case 'meta':
      handlers.onMeta?.(String(data.session_id ?? ''))
      break
    case 'node':
      handlers.onNode?.(String(data.node ?? ''))
      break
    case 'delta':
      handlers.onDelta?.(String(data.text ?? ''))
      break
    case 'replace':
      handlers.onReplace?.(String(data.text ?? ''))
      break
    case 'done':
      handlers.onDone?.(data as unknown as StreamDone)
      break
  }
}
