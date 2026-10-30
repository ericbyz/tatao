import { useEffect, useMemo, useState } from 'react'
import { createRun, fetchAgentModel, type AgentModelInfo } from '../app/api'
import type { AgentConfig, Environment, Project, TestCase, TestRun } from '../app/types'
import { Modal } from '../components/Modal'

type ExecuteRunDialogProps = {
  projects: Project[]
  environments: Environment[]
  agents: AgentConfig[]
  testCases: TestCase[]
  defaultProjectId: string
  onClose: () => void
  onCreated: (run: TestRun) => void
  notify: (message: string, tone?: 'success' | 'error') => void
}

// ExecuteRunDialog 是「一键测试」的执行配置对话框：选项目、测试环境、
// 勾用例、选智能体与模型，由后端创建批次并交智能体逐条执行。
export function ExecuteRunDialog({ projects, environments, agents, testCases, defaultProjectId, onClose, onCreated, notify }: ExecuteRunDialogProps) {
  const [projectId, setProjectId] = useState(defaultProjectId || projects[0]?.id || '')
  const [environmentId, setEnvironmentId] = useState('') // 空 = 本机
  const [agentId, setAgentId] = useState(agents.find((agent) => agent.isDefault)?.id ?? agents[0]?.id ?? '')
  const [model, setModel] = useState('')
  const [modelOptions, setModelOptions] = useState<AgentModelInfo | null>(null)
  const [workspaceDir, setWorkspaceDir] = useState(projects.find((project) => project.id === projectId)?.localPath ?? '')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [starting, setStarting] = useState(false)

  const projectCases = useMemo(() => testCases.filter((testCase) => testCase.projectId === projectId), [testCases, projectId])
  const currentAgent = agents.find((agent) => agent.id === agentId)
  const isCLIAgent = currentAgent?.type === 'claude' || currentAgent?.type === 'codex' || currentAgent?.type === 'zcode' || currentAgent?.type === 'custom'
  const selectedEnvironment = environments.find((environment) => environment.id === environmentId)

  // 切换 CLI 智能体时读取其本机配置的默认模型，供显式指定
  useEffect(() => {
    if (!currentAgent || !isCLIAgent) {
      setModelOptions(null)
      return
    }
    let cancelled = false
    fetchAgentModel(currentAgent.type)
      .then((info) => {
        if (!cancelled) setModelOptions(info)
      })
      .catch(() => setModelOptions(null))
    return () => { cancelled = true }
  }, [currentAgent, isCLIAgent])

  const pickProject = (nextId: string) => {
    setProjectId(nextId)
    setSelected(new Set())
    setWorkspaceDir(projects.find((project) => project.id === nextId)?.localPath ?? '')
  }

  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const submit = async () => {
    if (!projectId || selected.size === 0 || !agentId || !workspaceDir.trim()) return
    setStarting(true)
    try {
      const run = await createRun({
        projectId,
        caseIds: [...selected],
        agentId,
        workspaceDir: workspaceDir.trim(),
        environmentId: environmentId || undefined,
        model: model || undefined,
      })
      onCreated(run)
    } catch (error) {
      notify(error instanceof Error ? error.message : '启动执行失败', 'error')
      setStarting(false)
    }
  }

  const dirLabel = environmentId ? `项目在环境「${selectedEnvironment?.name ?? ''}」上的路径` : '项目路径（本机）'
  const modelPlaceholder = isCLIAgent ? (modelOptions?.configured ? modelOptions.model : '智能体默认模型') : '例如 deepseek-chat'
  const modelChoices = useMemo(() => {
    const set = new Set<string>()
    if (modelOptions?.model) set.add(modelOptions.model)
    for (const model of modelOptions?.knownModels ?? []) set.add(model)
    return [...set]
  }, [modelOptions])

  return (
    <Modal title="一键测试" description="选择测试环境与用例，交由智能体实际执行并收集每条结论。" onClose={onClose} width="wide">
      <div className="form-stack">
        <div className="form-grid">
          <label className="field"><span>项目</span>
            <select value={projectId} onChange={(event) => pickProject(event.target.value)}>
              {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
            </select>
          </label>
          <label className="field"><span>测试环境</span>
            <select value={environmentId} onChange={(event) => setEnvironmentId(event.target.value)}>
              <option value="">本机（服务所在机器）</option>
              {environments.map((environment) => <option key={environment.id} value={environment.id}>{environment.name}（{environment.host}）</option>)}
            </select>
          </label>
          <label className="field"><span>智能体</span>
            {agents.length === 0 ? (
              <select disabled><option>未配置智能体</option></select>
            ) : (
              <select value={agentId} onChange={(event) => setAgentId(event.target.value)}>
                {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}（{agent.type}）</option>)}
              </select>
            )}
          </label>
          <label className="field"><span>模型</span>
            {isCLIAgent ? (
              <>
                <input value={model} onChange={(event) => setModel(event.target.value)} placeholder={modelPlaceholder} list="execute-model-options" />
                <datalist id="execute-model-options">
                  {modelChoices.map((choice) => <option key={choice} value={choice} />)}
                </datalist>
              </>
            ) : currentAgent ? (
              <input value={model} onChange={(event) => setModel(event.target.value)} placeholder={currentAgent.model || '接口模型名'} />
            ) : null}
          </label>
          <label className="field field--full"><span>{dirLabel}</span><input value={workspaceDir} onChange={(event) => setWorkspaceDir(event.target.value)} placeholder={environmentId ? '例如 /opt/app/under-test' : '智能体将在此目录内实际执行验证'} required /></label>
          {agents.length === 0 ? <small className="field field--full muted-text">还没有可用的智能体，请先到「连接设置 → AI 智能体」添加（支持本机 Claude Code / Codex）。</small> : null}
          {environmentId ? <small className="field field--full muted-text">将在环境「{selectedEnvironment?.name}」上通过 SSH 执行，要求该环境已安装对应的 CLI 智能体（{currentAgent?.type}）。</small> : null}
        </div>
        <div className="case-picker">
          <div className="case-picker__head">
            <span>选择用例（{projectCases.length}）</span>
            <button className="button button--small" type="button" onClick={() => setSelected(new Set(projectCases.map((c) => c.id)))}>全选</button>
          </div>
          <div className="case-picker__list">
            {projectCases.map((testCase) => (
              <label key={testCase.id} className="assignment-option">
                <input type="checkbox" checked={selected.has(testCase.id)} onChange={() => toggle(testCase.id)} />
                <span>
                  <strong>{testCase.title}</strong>
                  <small>{testCase.source} · {testCase.priority} · {testCase.steps.length} 步</small>
                </span>
              </label>
            ))}
            {projectCases.length === 0 ? <div className="assignment-empty">该项目还没有用例，请先生成或手动创建。</div> : null}
          </div>
        </div>
      </div>
      <footer className="modal__footer">
        <button className="button" type="button" onClick={onClose}>取消</button>
        <button className="button button--primary" type="button" disabled={starting || selected.size === 0 || !workspaceDir.trim() || !agentId} onClick={() => void submit()}>
          {starting ? '启动中…' : `开始执行${selected.size ? `（${selected.size} 条）` : ''}`}
        </button>
      </footer>
    </Modal>
  )
}
