export type PageKey = 'home' | 'projects' | 'environments' | 'cases' | 'runs' | 'connections'

export type Project = {
  id: string
  name: string
  source: 'local' | 'tapd'
  localPath?: string
  tapdWorkspaceId?: string
  context?: string
  environmentCount: number
  caseCount: number
  status: 'ready' | 'needs_setup'
  updatedAt: string
}

export type Environment = {
  id: string
  projectIds: string[]
  name: string
  os: 'linux'
  host: string
  port: number
  username: string
  authType: 'password' | 'ssh_key'
  notes?: string
  explorePrompt?: string
  secretSet: boolean
  status: 'online' | 'unchecked' | 'offline'
}

export type TestCase = {
  id: string
  projectId: string
  title: string
  priority: 'P0' | 'P1' | 'P2'
  source: string
  externalId?: string
  preconditions?: string
  steps: string[]
  expected: string
  status: 'ready' | 'draft'
}

export type AgentType = 'claude' | 'codex' | 'zcode' | 'custom' | 'openai'

export type AgentConfig = {
  id: string
  name: string
  type: AgentType
  baseUrl?: string
  model?: string
  command?: string
  extraArgs?: string
  apiKeySet: boolean
  isDefault: boolean
}

export type AgentInput = {
  id?: string
  name: string
  type: AgentType
  baseUrl?: string
  model?: string
  command?: string
  extraArgs?: string
  apiKey?: string
  isDefault: boolean
}

export type LocalAgent = { type: AgentType; name: string; available: boolean; path?: string }

export type TAPDStatus = {
  configured: boolean
  connected: boolean
  transport: string
  message: string
}

export type TAPDSettings = {
  transport: 'openapi' | 'mcp'
  baseUrl: string
  authType: 'basic' | 'bearer'
  apiUser: string
  companyId: string
  workspaceIds: string[]
  endpoint: string
  hasSecret: boolean
}

export type TAPDSettingsInput = Partial<TAPDSettings> & { apiPassword?: string }

export type TAPDWorkspace = { id: string; name: string }

export type TAPDLocalProvider = { source: string; kind: string; endpoint: string }

export type ExternalItem = {
  id: string
  projectId: string
  kind: 'bug' | 'requirement'
  title: string
  description: string
  status: string
  priority?: string
}

export type SyncSummary = { total: number; bugs: number; requirements: number }

export type RunCaseResult = {
  caseId: string
  title: string
  source: string
  status: 'passed' | 'failed' | 'error'
  summary: string
  evidence?: string
  durationMs: number
}

export type TestRun = {
  id: string
  projectId: string
  agentId: string
  agentName: string
  model: string
  environmentId: string
  environmentName: string
  workspaceDir: string
  status: 'running' | 'completed' | 'failed'
  total: number
  passed: number
  failed: number
  errors: number
  currentTitle?: string
  createdAt: string
  finishedAt: string
  results: RunCaseResult[]
}

export type TestCaseInput = {
  projectId: string
  title: string
  priority: 'P0' | 'P1' | 'P2'
  preconditions?: string
  steps: string[]
  expected: string
}

export type KnowledgeCategory = '功能导航' | '数据与文件约定' | '接口与服务' | '账号与凭据' | '术语解释' | '禁忌与风险'

export type KnowledgeEntry = {
  id: string
  projectId: string
  category: string
  title: string
  content: string
  source?: 'manual' | 'ai'
  generatedBy?: string
  createdAt: string
}
