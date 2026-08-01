<template>
  <div class="panel">
    <h2 class="panel-title">代码问答</h2>
    <p v-if="!repoUrl" class="placeholder-text">请先摄取一个仓库</p>
    <template v-else>
      <div class="repo-tag">
        <span class="tag-icon">📦</span>
        <span class="tag-text">{{ repoUrl }}</span>
      </div>
      <div class="input-row">
        <textarea
          v-model="question"
          placeholder="提出关于代码的问题..."
          class="question-input"
          rows="3"
          :disabled="generating"
          @keydown.ctrl.enter="send"
        />
        <button
          class="btn btn-primary send-btn"
          :disabled="!question.trim() || generating"
          @click="send"
        >
          {{ generating ? '生成中...' : '发送' }}
        </button>
      </div>
      <div v-if="answerText" class="answer-area">
        <div class="answer-content">{{ answerText }}</div>
        <div v-if="sources.length" class="sources-section">
          <h3 class="sources-title">引用来源</h3>
          <div v-for="(s, i) in sources" :key="i" class="source-item">
            <div class="source-meta">
              <span class="source-lang">{{ s.language }}</span>
              <span class="source-path">{{ s.file_path }}</span>
            </div>
            <div class="source-range">行 {{ s.start_line }} - {{ s.end_line }}</div>
            <div class="source-score">相似度: {{ (s.score * 100).toFixed(1) }}%</div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { askStream, type SourceRef } from '../api'

const props = defineProps<{ repoUrl: string }>()

const question = ref('')
const generating = ref(false)
const answerText = ref('')
const sources = ref<SourceRef[]>([])

watch(() => props.repoUrl, () => {
  question.value = ''
  answerText.value = ''
  sources.value = []
})

async function send() {
  const q = question.value.trim()
  if (!q || !props.repoUrl) return

  generating.value = true
  answerText.value = ''
  sources.value = []

  try {
    for await (const event of askStream(props.repoUrl, q)) {
      if (event.type === 'error') {
        answerText.value = `❌ ${event.message}`
        generating.value = false
        return
      }
      if (event.type === 'token') {
        answerText.value += event.content
      }
      if (event.type === 'sources') {
        sources.value = event.sources
      }
      if (event.type === 'done') {
        generating.value = false
        return
      }
    }
  } catch (e: any) {
    answerText.value = `❌ 错误: ${e.message}`
  } finally {
    generating.value = false
  }
}
</script>

<style scoped>
.panel {
  background: #fff;
  border-radius: 12px;
  padding: 20px 24px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.08);
}

.panel-title {
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 14px;
}

.placeholder-text {
  color: #9ca3af;
  font-size: 14px;
  text-align: center;
  padding: 24px 0;
}

.repo-tag {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: #f3f4f6;
  border-radius: 8px;
  margin-bottom: 14px;
  font-size: 13px;
}

.tag-icon {
  font-size: 14px;
}

.tag-text {
  font-weight: 500;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: calc(100% - 24px);
}

.input-row {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}

.question-input {
  flex: 1;
  padding: 10px 12px;
  border: 1px solid #d1d5db;
  border-radius: 8px;
  font-size: 14px;
  resize: vertical;
  outline: none;
  transition: border-color 0.2s;
  font-family: inherit;
}

.question-input:focus {
  border-color: #4f46e5;
}

.btn {
  padding: 9px 18px;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.2s;
  white-space: nowrap;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background: #4f46e5;
  color: #fff;
}

.btn-primary:hover:not(:disabled) {
  background: #4338ca;
}

.send-btn {
  align-self: flex-end;
}

.answer-area {
  background: #f9fafb;
  border-radius: 8px;
  padding: 14px 16px;
  border: 1px solid #e5e7eb;
}

.answer-content {
  font-size: 14px;
  line-height: 1.7;
  color: #1f2937;
  white-space: pre-wrap;
  margin-bottom: 14px;
}

.sources-section {
  border-top: 1px solid #e5e7eb;
  padding-top: 12px;
}

.sources-title {
  font-size: 13px;
  font-weight: 600;
  color: #6b7280;
  margin-bottom: 10px;
}

.source-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  background: #fff;
  border-radius: 6px;
  margin-bottom: 6px;
  font-size: 12px;
}

.source-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.source-lang {
  padding: 2px 6px;
  background: #ede9fe;
  color: #6d28d9;
  border-radius: 4px;
  font-weight: 500;
  font-size: 11px;
}

.source-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #6b7280;
}

.source-range {
  color: #9ca3af;
  margin: 0 10px;
}

.source-score {
  color: #059669;
  font-weight: 500;
}
</style>