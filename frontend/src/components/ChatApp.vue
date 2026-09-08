<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { checkHealth, sendChat } from '../api/client'
import type { ChatMessage } from '../types/chat'

const USER_ID = 'web_user_001'

const messages = ref<ChatMessage[]>([
  {
    id: 'welcome',
    role: 'assistant',
    content:
      '你好，我是 Smart CS 智能客服。可以询问理财产品、退款政策、开户流程，查询订单（如 ORD-2024-001），或申请工单处理。',
  },
])
const draft = ref('')
const sessionId = ref<string | undefined>()
const sending = ref(false)
const online = ref(false)
const errorText = ref('')
const listRef = ref<HTMLElement | null>(null)

const suggestions = [
  '理财产品收益怎么样？',
  '查一下订单 ORD-2024-001',
  '开户需要准备什么？',
  '我想申请退款',
]

const canSend = computed(() => draft.value.trim().length > 0 && !sending.value)

function uid() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

async function scrollToBottom() {
  await nextTick()
  const el = listRef.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(messages, () => {
  void scrollToBottom()
}, { deep: true })

onMounted(async () => {
  try {
    const health = await checkHealth()
    online.value = health.status === 'healthy'
  } catch {
    online.value = false
  }
})

async function submit(text?: string) {
  const content = (text ?? draft.value).trim()
  if (!content || sending.value) return

  errorText.value = ''
  draft.value = ''
  sending.value = true

  messages.value.push({
    id: uid(),
    role: 'user',
    content,
  })

  const pendingId = uid()
  messages.value.push({
    id: pendingId,
    role: 'assistant',
    content: '正在编排 Agent…',
    pending: true,
  })

  try {
    const res = await sendChat({
      message: content,
      user_id: USER_ID,
      session_id: sessionId.value,
    })
    sessionId.value = res.session_id
    const idx = messages.value.findIndex((m) => m.id === pendingId)
    if (idx >= 0) {
      messages.value[idx] = {
        id: pendingId,
        role: 'assistant',
        content: res.response,
        intent: res.intent,
        toolsUsed: res.tools_used,
        compliancePassed: res.compliance_passed,
      }
    }
    online.value = true
  } catch (err) {
    messages.value = messages.value.filter((m) => m.id !== pendingId)
    errorText.value = err instanceof Error ? err.message : '发送失败'
    online.value = false
  } finally {
    sending.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    void submit()
  }
}

function intentLabel(intent?: string) {
  if (!intent) return ''
  const map: Record<string, string> = {
    knowledge_rag: '知识检索',
    ticket_handler: '工单处理',
    tool_agent: '工具调用',
    chitchat: '闲聊接待',
    compliance_checker: '合规审查',
  }
  return map[intent] ?? intent
}
</script>

<template>
  <div class="shell">
    <div class="aurora" aria-hidden="true" />
    <div class="grain" aria-hidden="true" />

    <main class="stage">
      <header class="brand-bar">
        <div class="brand">
          <span class="mark" aria-hidden="true">SC</span>
          <div class="brand-copy">
            <h1>Smart CS</h1>
            <p>多 Agent 智能客服 · Go / Eino</p>
          </div>
        </div>
        <div class="status" :data-online="online">
          <span class="dot" />
          {{ online ? '服务在线' : '后端离线' }}
        </div>
      </header>

      <section class="chat" aria-label="对话区域">
        <div ref="listRef" class="messages">
          <article
            v-for="msg in messages"
            :key="msg.id"
            class="bubble"
            :class="[msg.role, { pending: msg.pending }]"
          >
            <div class="meta">
              <span>{{ msg.role === 'user' ? '你' : 'Smart CS' }}</span>
              <span v-if="msg.intent" class="chip">{{ intentLabel(msg.intent) }}</span>
              <span
                v-for="tool in msg.toolsUsed || []"
                :key="tool"
                class="chip"
              >{{ tool }}</span>
              <span
                v-if="msg.compliancePassed === false"
                class="chip warn"
              >合规拦截</span>
            </div>
            <p class="text">{{ msg.content }}</p>
          </article>
        </div>

        <div class="composer-wrap">
          <div v-if="!messages.some(m => m.role === 'user')" class="hints">
            <button
              v-for="item in suggestions"
              :key="item"
              type="button"
              class="hint"
              :disabled="sending"
              @click="submit(item)"
            >
              {{ item }}
            </button>
          </div>

          <p v-if="errorText" class="error">{{ errorText }}</p>

          <form class="composer" @submit.prevent="submit()">
            <textarea
              v-model="draft"
              rows="1"
              placeholder="输入问题，例如：理财产品年化收益是多少？"
              :disabled="sending"
              @keydown="onKeydown"
            />
            <button type="submit" class="send" :disabled="!canSend">
              {{ sending ? '发送中' : '发送' }}
            </button>
          </form>
          <p class="footnote">
            Enter 发送 · Shift+Enter 换行
            <template v-if="sessionId"> · 会话 {{ sessionId }}</template>
          </p>
        </div>
      </section>
    </main>
  </div>
</template>
