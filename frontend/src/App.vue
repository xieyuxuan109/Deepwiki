<template>
  <div class="app">
    <header class="header">
      <h1>DeepWiki</h1>
      <p class="subtitle">Git 仓库代码知识库问答系统</p>
    </header>
    <main class="main">
      <IngestPanel @ingested="onIngested" />
      <AskPanel :repo-url="currentRepoUrl" :key="currentRepoUrl" />
    </main>
  </div>
</template>

<script setup lang="ts">
import IngestPanel from './components/IngestPanel.vue'
import AskPanel from './components/AskPanel.vue'
import { ref } from 'vue'

const currentRepoUrl = ref('')

function onIngested(url: string) {
  currentRepoUrl.value = url
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: 'Noto Sans CJK SC', system-ui, -apple-system, sans-serif;
  background: #f5f7fa;
  color: #1a1a2e;
  min-height: 100vh;
}

.app {
  max-width: 900px;
  margin: 0 auto;
  padding: 32px 20px;
}

.header {
  text-align: center;
  margin-bottom: 32px;
}

.header h1 {
  font-size: 28px;
  font-weight: 700;
  color: #1a1a2e;
}

.subtitle {
  color: #6b7280;
  font-size: 14px;
  margin-top: 4px;
}

.main {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
</style>
