import type {
  AgentConfig,
  AgentInput,
  LocalAgent,
  TestCaseInput,
  TestRun,
  Environment,
  ExternalItem,
  Project,
  SyncSummary,
  TAPDLocalProvider,
  TAPDSettings,
  TAPDSettingsInput,
  TAPDStatus,
  TAPDWorkspace,
  TestCase,
} from './types'

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })
  const body: unknown = await response.json()
  if (!response.ok) {
    const message = typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string'
      ? body.error
      : `请求失败 (${response.status})`
    throw new Error(message)
  }
  return body as T
}

export function loadWorkspace(signal?: AbortSignal) {
  return Promise.all([
    request<Project[]>('/api/projects', { signal }),
    request<Environment[]>('/api/environments', { signal }),
    request<TestCase[]>('/api/test-cases', { signal }),
    request<TAPDStatus>('/api/tapd/status', { signal }),
    request<TAPDSettings>('/api/tapd/settings', { signal }),
    request<AgentConfig[]>('/api/agents', { signal }),
  ])
}

export function createProject(input: Pick<Project, 'name' | 'source'> & Partial<Project>) {
  return request<Project>('/api/projects', { method: 'POST', body: JSON.stringify(input) })
}

export function updateProject(id: string, input: { name?: string; context: string }) {
  return request<Project>('/api/projects/' + id, { method: 'PATCH', body: JSON.stringify(input) })
}

export function createEnvironment(input: Omit<Environment, 'id' | 'projectIds' | 'os' | 'secretSet' | 'status'> & { secret: string }) {
  return request<Environment>('/api/environments', { method: 'POST', body: JSON.stringify(input) })
}

export function assignEnvironment(environmentId: string, projectIds: string[]) {
  return request<Environment>(`/api/environments/${environmentId}/projects`, {
    method: 'PATCH',
    body: JSON.stringify({ projectIds }),
  })
}

export function createTestCase(input: Omit<TestCase, 'id'>) {
  return request<TestCase>('/api/test-cases', { method: 'POST', body: JSON.stringify(input) })
}

export function syncTAPD(projectId: string) {
  return request<SyncSummary>('/api/tapd/sync', {
    method: 'POST',
    body: JSON.stringify({ projectId, kinds: ['bug', 'requirement'] }),
  })
}

export function loadExternalItems(projectId?: string, signal?: AbortSignal) {
  const query = projectId ? `?projectId=${encodeURIComponent(projectId)}` : ''
  return request<ExternalItem[]>(`/api/external-items${query}`, { signal })
}

export function generateTestCases(projectId: string, itemIds: string[], agentId?: string, model?: string) {
  return request<TestCase[]>('/api/test-cases/generate', {
    method: 'POST',
    body: JSON.stringify({ projectId, itemIds, ...(agentId ? { agentId } : {}), ...(model ? { model } : {}) }),
  })
}

export function listAgents(signal?: AbortSignal) {
  return request<AgentConfig[]>('/api/agents', { signal })
}

export function saveAgent(input: AgentInput) {
  return request<AgentConfig>('/api/agents', { method: 'POST', body: JSON.stringify(input) })
}

export function deleteAgent(id: string) {
  return request<{ deleted: boolean }>(`/api/agents/${id}`, { method: 'DELETE' })
}

export function testAgent(input: AgentInput & { id?: string }) {
  return request<{ ok: boolean; message: string }>('/api/agents/test', { method: 'POST', body: JSON.stringify(input) })
}

export function listLocalAgents() {
  return request<LocalAgent[]>('/api/agents/local')
}

export function listTAPDWorkspaces(signal?: AbortSignal) {
  return request<TAPDWorkspace[]>('/api/tapd/workspaces', { signal })
}

export function listLocalMCPProviders() {
  return request<TAPDLocalProvider[]>('/api/tapd/local-mcp')
}

export function saveTAPDSettings(input: TAPDSettingsInput) {
  return request<{ settings: TAPDSettings; status: TAPDStatus }>('/api/tapd/settings', {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export function testTAPDSettings(input: TAPDSettingsInput) {
  return request<TAPDStatus>('/api/tapd/test', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function listRuns(projectId?: string, signal?: AbortSignal) {
  const query = projectId ? '?projectId=' + encodeURIComponent(projectId) : ''
  return request<TestRun[]>('/api/runs' + query, { signal })
}

export function getRun(id: string, signal?: AbortSignal) {
  return request<TestRun>('/api/runs/' + id, { signal })
}

export function createRun(input: { projectId: string; caseIds: string[]; agentId: string; workspaceDir: string; environmentId?: string; model?: string }) {
  return request<TestRun>('/api/runs', { method: 'POST', body: JSON.stringify(input) })
}

export type AgentModelInfo = { model: string; source: string; configured: boolean; knownModels: string[] }

export function fetchAgentModel(agentType: string, signal?: AbortSignal) {
  return request<AgentModelInfo>('/api/agents/model?type=' + encodeURIComponent(agentType), { signal })
}

export function createManualTestCase(input: TestCaseInput) {
  return request<TestCase>('/api/test-cases', { method: 'POST', body: JSON.stringify(input) })
}
