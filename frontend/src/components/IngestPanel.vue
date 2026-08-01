<template>
  <div class="panel">
    <h2 class="panel-title">摄取仓库</h2>
    <div class="form-row">
      <input
        v-model="repoUrl"
        type="text"
        placeholder="https://github.com/user/repo"
        class="input"
        :disabled="loading"
      />
      <button
        class="btn btn-primary"
        :disabled="!repoUrl.trim() || loading"
        @click="submit"
      >
        {{ loading ? '处理中...' : '开始摄取' }}
      </button>
    </div>
    <div v-if="task" class="status-card" :class="task.status">
      <div class="status-header">
        <span class="status-badge" :class="task.status">{{ statusLabel(task.status) }}</span>
        <span class="status-msg">{{ task.message }}</span>
      </div>
      <div class="progress-bar-wrap">
        <div class="progress-bar" :style="{ width: (task.progress * 100) + '%' }"></div>
      </div>
      <div v-if="task.error" class="error-text">{{ task.error }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { startIngest, getIngestStatus, type IngestTask } from '../api'

const emit = defineEmits<{ ingested: [url: string] }>()

const repoUrl = ref('')
const loading = ref(false)
const task = ref<IngestTask | null>(null)

const statusLabels: Record<string, string> = {
  pending: '等待中',
  running: '执行中',
  completed: '已完成',
  failed: '失败',
}

function statusLabel(s: string) {
  return statusLabels[s] || s
}

async function submit() {
  const url = repoUrl.value.trim()
  if (!url) return
  loading.value = true
  task.value = null

  try {
    const taskId = await startIngest(url)
    const poll = setInterval(async () => {
      const t = await getIngestStatus(taskId)
      task.value = t
      if (t.status === 'completed' || t.status === 'failed') {
        clearInterval(poll)
        loading.value = false
        if (t.status === 'completed') {
          emit('ingested', url)
        }
      }
    }, 1500)
  } catch (e: any) {
    task.value = { id: '', repo_url: url, status: 'failed', progress: 0, message: '提交失败', total_files: 0, processed_files: 0, error: e.message }
    loading.value = false
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

.form-row {
  display: flex;
  gap: 10px;
}

.input {
  flex: 1;
  padding: 9px 12px;
  border: 1px solid #d1d5db;
  border-radius: 8px;
  font-size: 14px;
  outline: none;
  transition: border-color 0.2s;
}

.input:focus {
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

.status-card {
  margin-top: 14px;
  padding: 12px 16px;
  border-radius: 8px;
  background: #f9fafb;
  border-left: 3px solid #d1d5db;
}

.status-card.running {
  border-left-color: #3b82f6;
}

.status-card.completed {
  border-left-color: #22c55e;
}

.status-card.failed {
  border-left-color: #ef4444;
  background: #fef2f2;
}

.status-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.status-badge {
  font-size: 12px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  color: #fff;
}

.status-badge.pending { background: #9ca3af; }
.status-badge.running { background: #3b82f6; }
.status-badge.completed { background: #22c55e; }
.status-badge.failed { background: #ef4444; }

.status-msg {
  font-size: 13px;
  color: #6b7280;
}

.progress-bar-wrap {
  height: 6px;
  background: #e5e7eb;
  border-radius: 3px;
  overflow: hidden;
}

.progress-bar {
  height: 100%;
  background: #4f46e5;
  border-radius: 3px;
  transition: width 0.4s ease;
}

.error-text {
  margin-top: 8px;
  font-size: 13px;
  color: #ef4444;
}
</style>
