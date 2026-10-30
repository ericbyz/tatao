import { useEffect, useState, type FormEvent } from 'react'
import { listLocalAgents, listLocalMCPProviders } from '../app/api'
import type { AgentConfig, AgentInput, LocalAgent, Project, TAPDSettings, TAPDStatus } from '../app/types'
import { request } from '../app/api'
import { Icon } from '../components/Icon'
import { StatusTag } from '../components/StatusTag'

export function HomeView({ projects }: { projects: Project[] }) {
  const ready = projects.filter((project) => project.status === 'ready').length
  return <main className="page-content page-content--wide"><header className="page-header"><div><h1>工作台</h1><p>从项目开始，逐步连接环境、用例和执行记录。</p></div></header><div className="overview-list"><div><span>项目</span><strong>{projects.length}</strong><small>{ready} 个已就绪</small></div><div><span>下一步</span><strong>配置 Linux 环境</strong><small>验证连接后即可创建测试执行</small></div><div><span>TAPD</span><strong>连接设置</strong><small>配置后可同步缺陷与需求</small></div></div><section className="getting-started"><h2>开始使用</h2><ol><li><span>1</span><div><strong>建立项目</strong><p>选择本地文件夹或 TAPD 工作区作为来源。</p></div></li><li><span>2</span><div><strong>配置测试环境</strong><p>填写 Linux 地址和认证信息，执行连通性检查。</p></div></li><li><span>3</span><div><strong>准备测试用例</strong><p>手工维护，或从 TAPD Bug / 需求生成草稿。</p></div></li></ol></section></main>
}

type ConnectionsViewProps = {
  settings: TAPDSettings | null
  status: TAPDStatus | null
  agents: AgentConfig[]
  onSave: (input: {
    transport: TAPDSettings['transport']
    baseUrl: string
    authType: TAPDSettings['authType']
    apiUser: string
    companyId: string
    workspaceIds: string[]
    endpoint: string
    apiPassword?: string
  }) => Promise<{ status: TAPDStatus }>
  onTest: (input: {
    transport: TAPDSettings['transport']
    baseUrl: string
    authType: TAPDSettings['authType']
    apiUser: string
    companyId: string
    workspaceIds: string[]
    endpoint: string
    apiPassword?: string
  }) => Promise<TAPDStatus>
  onSaveAgent: (input: AgentInput) => Promise<AgentConfig>
  onDeleteAgent: (id: string) => Promise<void>
  onTestAgent: (input: AgentInput) => Promise<string>
  notify: (message: string, tone?: 'success' | 'error') => void
}

type TAPDSettingsPayload = Parameters<ConnectionsViewProps['onSave']>[0]

export function ConnectionsView({ settings, status, agents, onSave, onTest, onSaveAgent, onDeleteAgent, onTestAgent, notify }: ConnectionsViewProps) {
  const [transport, setTransport] = useState<TAPDSettings['transport']>(settings?.transport ?? 'openapi')
  const [authType, setAuthType] = useState<TAPDSettings['authType']>(settings?.authType ?? 'basic')
  const [baseUrl, setBaseUrl] = useState(settings?.baseUrl ?? 'https://api.tapd.cn')
  const [apiUser, setApiUser] = useState(settings?.apiUser ?? '')
  const [companyId, setCompanyId] = useState(settings?.companyId ?? '')
  const [workspaceIdsText, setWorkspaceIdsText] = useState((settings?.workspaceIds ?? []).join(', '))
  const [apiPassword, setApiPassword] = useState('')
  const [endpoint, setEndpoint] = useState(settings?.endpoint ?? '')
  const [busy, setBusy] = useState<'test' | 'save' | 'import' | null>(null)
  const [probe, setProbe] = useState<TAPDStatus | null>(status)

  const payload = (): TAPDSettingsPayload => ({
    transport,
    baseUrl,
    authType,
    apiUser,
    companyId,
    workspaceIds: workspaceIdsText.split(/[,，\s]+/).map((id) => id.trim()).filter(Boolean),
    endpoint,
    ...(apiPassword ? { apiPassword } : {}),
  })

  const run = async (action: 'test' | 'save') => {
    setBusy(action)
    try {
      const nextStatus = action === 'test' ? await onTest(payload()) : (await onSave(payload())).status
      setProbe(nextStatus)
      if (action === 'save') {
        setApiPassword('')
        notify('TAPD 连接已保存')
      }
    } catch (error) {
      notify(error instanceof Error ? error.message : '操作失败', 'error')
    } finally {
      setBusy(null)
    }
  }

  const detectLocal = async () => {
    setBusy('import')
    try {
      const providers = await listLocalMCPProviders()
      const first = providers[0]
      if (!first) {
        notify('本机 MCP 配置中没有发现 TAPD 服务', 'error')
        return
      }
      setTransport('mcp')
      setEndpoint(first.endpoint)
      notify(`已从本机 ${first.kind} 配置导入 TAPD 服务地址`)
    } catch (error) {
      notify(error instanceof Error ? error.message : '读取本机配置失败', 'error')
    } finally {
      setBusy(null)
    }
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    void run('save')
  }

  const connected = probe?.connected ?? false

  return (
    <main className="page-content page-content--wide">
      <header className="page-header">
        <div><h1>连接设置</h1><p>管理外部系统连接和 MCP 内核状态。</p></div>
      </header>

      <form onSubmit={submit}>
        <div className="connection-row">
          <span className="connection-row__mark">T</span>
          <div>
            <h2>TAPD</h2>
            <p>同步工作区、缺陷和需求，用作测试用例素材。密钥只保存在服务端。</p>
          </div>
          <div className="connection-row__status">
            <StatusTag tone={connected ? 'success' : probe?.configured ? 'danger' : 'warning'}>
              {connected ? '已连接' : probe?.configured ? '连接失败' : '未配置'}
            </StatusTag>
            <small>{probe?.message ?? '尚未验证连接'}</small>
          </div>
        </div>

        <div className="settings-form">
          <div className="transport-switch" role="radiogroup" aria-label="传输方式">
            <button type="button" className={transport === 'openapi' ? 'is-active' : ''} onClick={() => setTransport('openapi')}>直连 OpenAPI</button>
            <button type="button" className={transport === 'mcp' ? 'is-active' : ''} onClick={() => setTransport('mcp')}>MCP 服务</button>
          </div>

          {transport === 'openapi' ? (
            <div className="form-grid">
              <label className="field field--full"><span>认证方式</span>
                <div className="transport-switch">
                  <button type="button" className={authType === 'basic' ? 'is-active' : ''} onClick={() => setAuthType('basic')}>API 账号</button>
                  <button type="button" className={authType === 'bearer' ? 'is-active' : ''} onClick={() => setAuthType('bearer')}>个人访问令牌</button>
                </div>
              </label>
              <label className="field"><span>服务地址</span><input value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} placeholder="https://api.tapd.cn" /></label>
              {authType === 'basic' ? (
                <label className="field"><span>API 账号</span><input value={apiUser} onChange={(event) => setApiUser(event.target.value)} placeholder="在 TAPD「公司管理 → 安全与集成」中创建" required /></label>
              ) : (
                <>
                  <label className="field"><span>公司 ID（可选）</span><input value={companyId} onChange={(event) => setCompanyId(event.target.value)} placeholder="TAPD 网页地址栏中的数字" /></label>
                  <label className="field field--full"><span>工作区 ID 清单（可选）</span><input value={workspaceIdsText} onChange={(event) => setWorkspaceIdsText(event.target.value)} placeholder="逗号分隔，例如 22616271, 37181055" /></label>
                  <small className="field field--full muted-text">个人访问令牌列全部项目需要公司 ID；未填时，平台会对上述工作区 ID 清单逐个向 TAPD 实时验证并显示真实项目列表。</small>
                </>
              )}
              <label className="field field--full"><span>{authType === 'basic' ? 'API 密码' : '访问令牌'}</span><input type="password" value={apiPassword} onChange={(event) => setApiPassword(event.target.value)} placeholder={settings?.hasSecret ? '已保存，留空则保持不变' : authType === 'basic' ? 'api_password' : 'TAPD 个人设置中创建的访问令牌'} /></label>
            </div>
          ) : (
            <div className="form-grid">
              <label className="field field--full"><span>MCP 服务地址或启动命令</span><input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder="https://host/mcp、https://…/sse/… 或 uvx mcp-server-tapd" required /></label>
              <label className="field"><span>API 账号（可选）</span><input value={apiUser} onChange={(event) => setApiUser(event.target.value)} /></label>
              <label className="field"><span>密码 / 访问令牌</span><input type="password" value={apiPassword} onChange={(event) => setApiPassword(event.target.value)} placeholder={settings?.hasSecret ? '已保存，留空则保持不变' : 'TAPD_ACCESS_TOKEN 或 API 密码'} /></label>
              <small className="field field--full muted-text">stdio 命令方式由后端启动子进程并注入 TAPD_API_USER / TAPD_API_PASSWORD 环境变量；http(s) 地址自动识别 Streamable HTTP 与 SSE 两种传输。</small>
              <button className="button button--small field--full" style={{ justifySelf: 'start' }} type="button" disabled={busy !== null} onClick={() => void detectLocal()}>
                <Icon name="search" size={15} /> {busy === 'import' ? '检测中…' : '从本机 Claude / Cursor 配置导入'}
              </button>
            </div>
          )}

          <footer className="settings-actions">
            <button className="button" type="button" disabled={busy !== null} onClick={() => void run('test')}>
              <Icon name="link" size={16} /> {busy === 'test' ? '测试中…' : '测试连接'}
            </button>
            <button className="button button--primary" type="submit" disabled={busy !== null}>
              {busy === 'save' ? '保存中…' : '保存并应用'}
            </button>
          </footer>
        </div>
      </form>

      <AgentsSection agents={agents} onSave={onSaveAgent} onDelete={onDeleteAgent} onTest={onTestAgent} notify={notify} />

      <div className="architecture-note">
        <Icon name="lock" size={20} />
        <div>
          <strong>内置 MCP 内核层</strong>
          <p>连接信息、工具发现、超时重试和审计封装在后端 Connector 中；前端不会直接持有 TAPD Token。</p>
        </div>
      </div>
    </main>
  )
}

type AgentsSectionProps = {
  agents: AgentConfig[]
  onSave: (input: AgentInput) => Promise<AgentConfig>
  onDelete: (id: string) => Promise<void>
  onTest: (input: AgentInput) => Promise<string>
  notify: (message: string, tone?: 'success' | 'error') => void
}

type AgentFormState = {
  id: string
  name: string
  type: AgentInput['type']
  baseUrl: string
  model: string
  command: string
  extraArgs: string
  apiKey: string
  isDefault: boolean
}

const emptyAgentForm: AgentFormState = {
  id: '', name: '', type: 'claude', baseUrl: '', model: '', command: '', extraArgs: '', apiKey: '', isDefault: true,
}

const agentTypeLabels: Record<string, string> = {
  claude: 'Claude Code（本机）',
  codex: 'Codex（本机）',
  zcode: 'ZCode（本机）',
  custom: '自定义命令',
  openai: 'OpenAI 兼容接口',
}

function AgentsSection({ agents, onSave: onSaveAgent, onDelete: onDeleteAgent, onTest: onTestAgent, notify }: AgentsSectionProps) {
  const [form, setForm] = useState<AgentFormState>({ ...emptyAgentForm })
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState<'save' | 'test' | 'delete' | null>(null)
  const [localAgents, setLocalAgents] = useState<LocalAgent[] | null>(null)
  const [modelHint, setModelHint] = useState('')
  const [modelTouched, setModelTouched] = useState(false)

  const payload = (): AgentInput => ({
    ...(form.id ? { id: form.id } : {}),
    name: form.name,
    type: form.type,
    model: form.model,
    extraArgs: form.extraArgs,
    ...(form.type === 'openai' ? { baseUrl: form.baseUrl, ...(form.apiKey ? { apiKey: form.apiKey } : {}) } : {}),
    ...(form.type === 'custom' ? { command: form.command } : {}),
    isDefault: form.isDefault,
  })

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy('save')
    try {
      const saved = await onSaveAgent(payload())
      notify('智能体「' + saved.name + '」已保存')
      setForm({ ...emptyAgentForm })
      setFormOpen(false)
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存智能体失败', 'error')
    } finally {
      setBusy(null)
    }
  }

  const runTest = async () => {
    setBusy('test')
    try {
      const message = await onTestAgent(payload())
      notify(message)
    } catch (error) {
      notify(error instanceof Error ? error.message : '测试失败', 'error')
    } finally {
      setBusy(null)
    }
  }

  const remove = async (agent: AgentConfig) => {
    setBusy('delete')
    try {
      await onDeleteAgent(agent.id)
      notify('智能体「' + agent.name + '」已删除')
    } catch (error) {
      notify(error instanceof Error ? error.message : '删除失败', 'error')
    } finally {
      setBusy(null)
    }
  }

  const edit = (agent: AgentConfig) => {
    setForm({
      id: agent.id,
      name: agent.name,
      type: agent.type,
      baseUrl: agent.baseUrl ?? '',
      model: agent.model ?? '',
      command: agent.command ?? '',
      extraArgs: agent.extraArgs ?? '',
      apiKey: '',
      isDefault: agent.isDefault,
    })
    setModelTouched(false)
    setFormOpen(true)
  }

  const detectLocal = async () => {
    try {
      setLocalAgents(await listLocalAgents())
    } catch (error) {
      notify(error instanceof Error ? error.message : '检测失败', 'error')
    }
  }

  const isCLI = form.type === 'claude' || form.type === 'codex' || form.type === 'zcode'

  // 切换 CLI 类型时读取本机智能体自身配置的默认模型并显式指定
  useEffect(() => {
    if (!formOpen || !isCLI) {
      setModelHint('')
      return
    }
    let cancelled = false
    request<{ model: string; source: string; configured: boolean }>('/api/agents/model?type=' + form.type)
      .then((info) => {
        if (cancelled) return
        if (info.configured) {
          if (!modelTouched) setForm((current) => ({ ...current, model: info.model }))
          setModelHint('已读取本机配置的默认模型 ' + info.model + '（' + info.source + '）')
        } else {
          setModelHint('本机 ' + form.type + ' 未显式配置模型，将使用其账号默认模型')
        }
      })
      .catch(() => setModelHint('读取本机模型配置失败'))
    return () => { cancelled = true }
  }, [form.type, formOpen, isCLI, modelTouched])

  return (
    <section className="agents-section">
      <div className="agents-head">
        <h2>AI 智能体</h2>
        <p>用于测试用例设计。可使用本机 CLI 智能体（复用其已登录的模型权限），或 OpenAI 兼容接口。</p>
        <button className="button button--small" type="button" onClick={() => void detectLocal()}>检测本机智能体</button>
        <button className="button button--small" type="button" onClick={() => { setForm({ ...emptyAgentForm }); setModelTouched(false); setFormOpen((value) => !value) }}>
          {formOpen ? '收起' : '新建智能体'}
        </button>
      </div>
      {localAgents ? (
        <div className="local-agents">
          {localAgents.map((local) => (
            <span key={local.type} className={local.available ? 'local-agent-chip is-ok' : 'local-agent-chip'}>
              {agentTypeLabels[local.type] ?? local.type}：{local.available ? '已安装' : '未安装'}
              {local.available
                ? <button type="button" onClick={() => { setForm({ ...emptyAgentForm, type: local.type, name: (agentTypeLabels[local.type] ?? local.type).replace('（本机）', '') }); setModelTouched(false); setFormOpen(true) }}>添加</button>
                : null}
            </span>
          ))}
        </div>
      ) : null}
      {formOpen ? (
        <form className="agent-form form-grid" onSubmit={submit}>
          <label className="field"><span>类型</span>
            <select value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value as AgentInput['type'] })}>
              {Object.entries(agentTypeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
          <label className="field"><span>名称</span><input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="例如：Claude Code 设计" required /></label>
          <label className="field"><span>模型（执行时显式指定）</span><input value={form.model} onChange={(event) => { setModelTouched(true); setForm({ ...form, model: event.target.value }) }} placeholder={isCLI ? '例如 sonnet' : '例如 deepseek-chat'} />{modelHint ? <small>{modelHint}</small> : null}</label>
          {form.type === 'openai' ? (
            <>
              <label className="field field--full"><span>服务地址（Base URL）</span><input value={form.baseUrl} onChange={(event) => setForm({ ...form, baseUrl: event.target.value })} placeholder="https://api.deepseek.com" required /></label>
              <label className="field"><span>API Key</span><input type="password" value={form.apiKey} onChange={(event) => setForm({ ...form, apiKey: event.target.value })} placeholder={form.id ? '已保存，留空则保持不变' : 'sk-…'} /></label>
            </>
          ) : null}
          {form.type === 'custom' ? (
            <label className="field field--full"><span>命令（{'{prompt}'} 为提示词占位符）</span><input value={form.command} onChange={(event) => setForm({ ...form, command: event.target.value })} placeholder="node designer.js --task {prompt}" required /></label>
          ) : null}
          <label className="field field--full"><span>附加命令行参数（可选）</span><input value={form.extraArgs} onChange={(event) => setForm({ ...form, extraArgs: event.target.value })} placeholder="例如 --model gpt-5" /></label>
          <label className="field checkbox-field"><input type="checkbox" checked={form.isDefault} onChange={(event) => setForm({ ...form, isDefault: event.target.checked })} /><span>设为默认智能体</span></label>
          <div className="settings-actions field--full">
            <button className="button" type="button" disabled={busy !== null} onClick={() => void runTest()}>{busy === 'test' ? '测试中…' : '测试连接'}</button>
            <button className="button button--primary" type="submit" disabled={busy !== null}>{busy === 'save' ? '保存中…' : '保存'}</button>
          </div>
        </form>
      ) : null}
      <div className="agents-list">
        {agents.map((agent) => (
          <div key={agent.id} className="agent-row">
            <div>
              <strong>{agent.name}{agent.isDefault ? ' · 默认' : ''}</strong>
              <small>{agentTypeLabels[agent.type] ?? agent.type}{agent.model ? ' · ' + agent.model : ''}{agent.baseUrl ? ' · ' + agent.baseUrl : ''}{agent.command ? ' · ' + agent.command : ''}{agent.type === 'openai' ? (agent.apiKeySet ? ' · 密钥已保存' : ' · 未配置密钥') : ''}</small>
            </div>
            <div className="agent-row__actions">
              <button className="button button--small" type="button" onClick={() => edit(agent)}>编辑</button>
              <button className="button button--small" type="button" disabled={busy !== null} onClick={() => void remove(agent)}>删除</button>
            </div>
          </div>
        ))}
        {agents.length === 0 ? <div className="empty-inline">还没有智能体。点「检测本机智能体」快速添加，或手动新建。</div> : null}
      </div>
    </section>
  )
}
