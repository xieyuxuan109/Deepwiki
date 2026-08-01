const BASE_URL = ''

export interface IngestTask {
  id: string
  repo_url: string
  status: string
  progress: number
  message: string
  total_files: number
  processed_files: number
  error?: string
}

export interface SourceRef {
  file_path: string
  start_line: number
  end_line: number
  language: string
  score: number
}

export async function startIngest(repoUrl: string, include?: string[], exclude?: string[]): Promise<string> {
  const res = await fetch(`${BASE_URL}/api/ingest`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_url: repoUrl, include, exclude }),
  })
  if (!res.ok) throw new Error(await res.text())
  const data = await res.json()
  return data.task_id
}

export async function getIngestStatus(taskId: string): Promise<IngestTask> {
  const res = await fetch(`${BASE_URL}/api/ingest/${taskId}/status`)
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function* askStream(repoUrl: string, question: string, topK = 5) {
  const res = await fetch(`${BASE_URL}/api/ask/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_url: repoUrl, question, top_k: topK }),
  })
  if (!res.ok) throw new Error(await res.text())

  const reader = res.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    const lines = buffer.split('\n')
    buffer = lines.pop() || ''

    for (const line of lines) {
      if (!line.startsWith('data:')) continue
      const jsonStr = line.slice(5).trim()
      if (!jsonStr) continue
      try {
        yield JSON.parse(jsonStr)
      } catch { /* skip */ }
    }
  }
}
