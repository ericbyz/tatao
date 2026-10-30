import { useEffect, useMemo, useState } from 'react'
import { createManualTestCase, fetchAgentModel } from '../app/api'
import type { AgentConfig, Environment, ExternalItem, Project, TestCase, TestCaseInput, TestRun } from '../app/types'
import { Icon } from '../components/Icon'
import { ExecuteRunDialog } from './ExecuteRunDialog'
import { Modal } from '../components/Modal'
import { StatusTag } from '../components/StatusTag'

type TestCasesViewProps = {
  projects: Project[]
  testCases: TestCase[]
  externalItems: ExternalItem[]
  agents: AgentConfig[]
  environments: Environment[]
  projectId: string
  onProjectChange: (projectId: string) => void
  onSync: (project: Project) => Promise<void>
  onGenerate: (projectId: string, itemIds: string[], agentId?: string, model?: string) => Promise<void>
  onCasesChanged: () => Promise<void>
  notify: (message: string, tone?: 'success' | 'error') => void
  onRunStarted?: (run: TestRun) => void
}

const PAGE_SIZE = 20

type KindFilter = 'all' | 'bug' | 'requirement'
type PriorityFilter = 'all' | 'P0' | 'P1' | 'P2'

export function TestCasesView({ projects, environments, testCases, externalItems, agents, projectId, onProjectChange, onSync, onGenerate, onCasesChanged, onRunStarted, notify }: TestCasesViewProps) {
  const [agentId, setAgentId] = useState('')
  const [model, setModel] = useState('')
  const [modelOptions, setModelOptions] = useState<{ model: string; knownModels: string[]; configured: boolean } | null>(null)
  const [tab, setTab] = useState<'cases' | 'tapd'>('cases')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [generating, setGenerating] = useState(false)
  const [query, setQuery] = useState('')
  const [projectFilter, setProjectFilter] = useState<'all' | string>('all')
  const [kindFilter, setKindFilter] = useState<KindFilter>('all')
  const [priorityFilter, setPriorityFilter] = useState<PriorityFilter>('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [caseDetail, setCaseDetail] = useState<TestCase | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [executeOpen, setExecuteOpen] = useState(false)
  const projectNames = useMemo(() => new Map(projects.map((project) => [project.id, project.name])), [projects])
  const currentProject = projects.find((project) => project.id === projectId) ?? projects[0]

  const statusOptions = useMemo(
    () => [...new Set(externalItems.map((item) => item.status).filter(Boolean))].sort(),
    [externalItems],
  )

  const filteredItems = useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase()
    return externalItems.filter((item) => {
      if (projectFilter !== 'all' && item.projectId !== projectFilter) return false
      if (kindFilter !== 'all' && item.kind !== kindFilter) return false
      if (priorityFilter !== 'all' && (item.priority || 'P2') !== priorityFilter) return false
      if (statusFilter !== 'all' && item.status !== statusFilter) return false
      if (keyword && !`${item.title} ${item.id}`.toLocaleLowerCase().includes(keyword)) return false
      return true
    })
  }, [externalItems, projectFilter, kindFilter, priorityFilter, statusFilter, query])

  const totalPages = Math.max(1, Math.ceil(filteredItems.length / PAGE_SIZE))
  const safePage = Math.min(page, totalPages)
  const pageItems = useMemo(
    () => filteredItems.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE),
    [filteredItems, safePage],
  )

  useEffect(() => {
    setPage(1)
  }, [projectFilter, kindFilter, priorityFilter, statusFilter, query])

  useEffect(() => {
    if (!agentId) {
      setModelOptions(null)
      setModel('')
      return
    }
    const agent = agents.find((a) => a.id === agentId)
    if (!agent || agent.type === 'openai') return
    let cancelled = false
    fetchAgentModel(agent.type).then((info) => {
      if (!cancelled) setModelOptions(info)
    }).catch(() => setModelOptions(null))
    return () => { cancelled = true }
  }, [agentId, agents])

  const toggle = (itemId: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(itemId)) {
        next.delete(itemId)
      } else {
        next.add(itemId)
      }
      return next
    })
  }

  const generate = async () => {
    if (selected.size === 0) return
    setGenerating(true)
    try {
      // 跨项目勾选时按项目分组生成，保持生成接口的单项目语义。
      const itemByProject = new Map(externalItems.map((item) => [item.id, item.projectId]))
      const byProject = new Map<string, string[]>()
      for (const id of selected) {
        const owner = itemByProject.get(id)
        if (!owner) continue
        byProject.set(owner, [...(byProject.get(owner) ?? []), id])
      }
      let created = 0
      for (const [ownerProjectId, itemIds] of byProject) {
        try {
          await onGenerate(ownerProjectId, itemIds, agentId || undefined, model || undefined)
          created += itemIds.length
        } catch (error) {
          notify(error instanceof Error ? error.message : '生成用例失败', 'error')
        }
      }
      if (created > 0) setSelected(new Set())
    } finally {
      setGenerating(false)
    }
  }

  return (
    <main className="page-content cases-page">
      <div className="cases-toolbar">
        <div className="tabs" role="tablist">
          <button className={tab === 'cases' ? 'is-active' : ''} type="button" onClick={() => setTab('cases')}>全部用例 · {testCases.length}</button>
          <button className={tab === 'tapd' ? 'is-active' : ''} type="button" onClick={() => setTab('tapd')}>TAPD 素材{externalItems.length > 0 ? ` · ${externalItems.length}` : ''}</button>
        </div>
        <div className="cases-toolbar__actions">
          <select className="toolbar-select" value={projectId} onChange={(event) => onProjectChange(event.target.value)} disabled={!projects.length} aria-label="同步目标项目">{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
          <button className="button button--small" type="button" disabled={!currentProject} onClick={() => setCreateOpen(true)}><Icon name="plus" size={16}/> 新建用例</button>
          <button className="button button--small button--primary" type="button" disabled={!currentProject} onClick={() => {
            if (agents.length === 0) {
              notify('请先到「连接设置 → AI 智能体」添加本机 Claude Code / Codex', 'error')
              return
            }
            setExecuteOpen(true)
          }}><Icon name="terminal" size={16}/> 一键测试</button>
          <button className="button button--small" type="button" disabled={!currentProject || currentProject.source !== 'tapd'} onClick={() => currentProject && onSync(currentProject)}><Icon name="refresh" size={16}/> 同步 TAPD</button>
        </div>
      </div>

      {tab === 'cases' ? (
        <div className="table-shell">
          <table className="data-table"><thead><tr><th>用例标题</th><th>所属项目</th><th>优先级</th><th>来源</th><th>状态</th><th aria-label="操作"/></tr></thead>
            <tbody>{testCases.map((testCase) => (
              <tr key={testCase.id} className="is-clickable" onClick={() => setCaseDetail(testCase)}><td><span className="name-cell"><Icon name="clipboard" size={19}/>{testCase.title}</span></td><td>{projectNames.get(testCase.projectId)}</td><td><span className={`priority priority--${testCase.priority.toLowerCase()}`}>{testCase.priority}</span></td><td>{testCase.source}{testCase.externalId ? ` · ${testCase.externalId}` : ''}</td><td><StatusTag tone={testCase.status === 'ready' ? 'success' : 'neutral'}>{testCase.status === 'ready' ? '就绪' : '草稿'}</StatusTag></td><td><button className="row-action" type="button" aria-label="查看用例" onClick={(event) => { event.stopPropagation(); setCaseDetail(testCase) }}><Icon name="dots" size={18}/></button></td></tr>
            ))}</tbody></table>
          {testCases.length === 0 ? <div className="empty-inline">尚未添加测试用例。到「TAPD 素材」勾选后一键生成草稿。</div> : null}
        </div>
      ) : externalItems.length === 0 ? (
        <div className="external-empty"><Icon name="refresh" size={24}/><strong>尚未同步 TAPD 素材</strong><p>选择 TAPD 项目后点击「同步 TAPD」，缺陷和需求会显示在这里。</p><button className="button" type="button" disabled={!currentProject || currentProject.source !== 'tapd'} onClick={() => currentProject && onSync(currentProject)}>同步 TAPD</button></div>
      ) : (
        <div className="external-list">
          <div className="material-toolbar">
            <label className="search-field">
              <Icon name="search" size={17} />
              <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标题或 ID" />
            </label>
            <select className="toolbar-select" value={projectFilter} onChange={(event) => setProjectFilter(event.target.value)} aria-label="项目筛选">
              <option value="all">全部项目</option>
              {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
            </select>
            <select className="toolbar-select" value={kindFilter} onChange={(event) => setKindFilter(event.target.value as KindFilter)} aria-label="类型筛选">
              <option value="all">全部类型</option>
              <option value="bug">缺陷</option>
              <option value="requirement">需求</option>
            </select>
            <select className="toolbar-select" value={priorityFilter} onChange={(event) => setPriorityFilter(event.target.value as PriorityFilter)} aria-label="优先级筛选">
              <option value="all">全部优先级</option>
              <option value="P0">P0</option>
              <option value="P1">P1</option>
              <option value="P2">P2</option>
            </select>
            <select className="toolbar-select" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)} aria-label="状态筛选">
              <option value="all">全部状态</option>
              {statusOptions.map((status) => <option key={status} value={status}>{status}</option>)}
            </select>
            <select className="toolbar-select" value={agentId} onChange={(event) => setAgentId(event.target.value)} aria-label="用例设计智能体">
              <option value="">规则生成（内置）</option>
              {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name} · {agent.model}{agent.isDefault ? '（默认）' : ''}</option>)}
            </select>
            {agentId ? (
              <input className="toolbar-select" value={model} onChange={(event) => setModel(event.target.value)} aria-label="模型" placeholder="模型" list="gen-model-options" />
            ) : null}
            <datalist id="gen-model-options">
              {(modelOptions?.configured && modelOptions.model ? [modelOptions.model] : []).concat(modelOptions?.knownModels ?? []).filter((value, index, all) => all.indexOf(value) === index).map((choice) => <option key={choice} value={choice} />)}
            </datalist>
            <button className="button button--small button--primary" type="button" disabled={selected.size === 0 || generating} onClick={() => void generate()}>
              {generating ? '生成中…' : `生成测试用例${selected.size ? `（${selected.size}）` : ''}`}
            </button>
          </div>
          <div className="external-list__head">
            <span>已同步的缺陷与需求</span>
            <small>共 {filteredItems.length} 条{filteredItems.length !== externalItems.length ? `（全部 ${externalItems.length} 条）` : ''}</small>
          </div>
          <div className="external-rows">
            {pageItems.map((item) => (
              <label key={item.id} className="external-row external-row--select">
                <input type="checkbox" checked={selected.has(item.id)} onChange={() => toggle(item.id)} />
                <span className={`external-icon ${item.kind === 'bug' ? 'external-icon--bug' : ''}`}>{item.kind === 'bug' ? '缺' : '需'}</span>
                <div>
                  <strong>{item.title}</strong>
                  <small>{projectNames.get(item.projectId) ?? '未知项目'} · {item.kind === 'bug' ? '缺陷' : '需求'} · {item.id}{item.status ? ` · ${item.status}` : ''}{item.priority ? ` · ${item.priority}` : ''}</small>
                </div>
              </label>
            ))}
            {pageItems.length === 0 ? <div className="empty-inline">没有匹配的素材，请调整筛选条件。</div> : null}
          </div>
          <div className="pagination">
            <span>第 {safePage} / {totalPages} 页 · 每页 {PAGE_SIZE} 条</span>
            <button className="button button--small" type="button" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>上一页</button>
            <button className="button button--small" type="button" disabled={safePage >= totalPages} onClick={() => setPage(safePage + 1)}>下一页</button>
          </div>
        </div>
      )}

      {executeOpen ? (
        <ExecuteRunDialog
          projects={projects}
          environments={environments}
          agents={agents}
          testCases={testCases}
          defaultProjectId={currentProject?.id ?? ''}
          onClose={() => setExecuteOpen(false)}
          onCreated={(run) => {
            setExecuteOpen(false)
            notify('执行批次已开始，可到「执行记录」查看实时进度')
            onRunStarted?.(run)
          }}
          notify={notify}
        />
      ) : null}

      {createOpen && currentProject ? (
        <CreateCaseDialog
          projectId={currentProject.id}
          onClose={() => setCreateOpen(false)}
          onCreated={() => { setCreateOpen(false); void onCasesChanged() }}
          notify={notify}
        />
      ) : null}

      {caseDetail ? (
        <Modal title={caseDetail.title} description={`${projectNames.get(caseDetail.projectId) ?? ''} · ${caseDetail.source}${caseDetail.externalId ? ` · ${caseDetail.externalId}` : ''}`} onClose={() => setCaseDetail(null)} width="wide">
          <div className="case-detail">
            {caseDetail.preconditions ? <section><h3>前置条件</h3><p>{caseDetail.preconditions}</p></section> : null}
            <section><h3>测试步骤</h3><ol>{caseDetail.steps.map((step, index) => <li key={index}>{step}</li>)}</ol></section>
            <section><h3>预期结果</h3><p>{caseDetail.expected}</p></section>
          </div>
        </Modal>
      ) : null}
    </main>
  )
}

function CreateCaseDialog({ projectId, onClose, onCreated, notify }: { projectId: string; onClose: () => void; onCreated: () => void; notify: (message: string, tone?: 'success' | 'error') => void }) {
  const [title, setTitle] = useState('')
  const [priority, setPriority] = useState<'P0' | 'P1' | 'P2'>('P1')
  const [preconditions, setPreconditions] = useState('')
  const [stepsText, setStepsText] = useState('')
  const [expected, setExpected] = useState('')
  const [saving, setSaving] = useState(false)

  const submit = async () => {
    const steps = stepsText.split('\n').map((line) => line.trim()).filter(Boolean)
    if (!title.trim() || steps.length === 0) return
    setSaving(true)
    try {
      const input: TestCaseInput = {
        projectId,
        title: title.trim(),
        priority,
        preconditions: preconditions.trim() || undefined,
        steps,
        expected: expected.trim(),
      }
      await createManualTestCase(input)
      notify('用例已创建')
      onCreated()
    } catch (error) {
      notify(error instanceof Error ? error.message : '创建失败', 'error')
      setSaving(false)
    }
  }

  return (
    <Modal title="新建用例" description="手动编写的用例，来源将标记为「手动创建」，同样可交由智能体一键执行。" onClose={onClose} width="wide">
      <div className="form-stack">
        <div className="form-grid">
          <label className="field field--full"><span>用例标题</span><input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="例如：验证大文件同步断点续传" required /></label>
          <label className="field"><span>优先级</span>
            <select value={priority} onChange={(event) => setPriority(event.target.value as 'P0' | 'P1' | 'P2')}>
              <option value="P0">P0</option><option value="P1">P1</option><option value="P2">P2</option>
            </select>
          </label>
          <label className="field"><span>前置条件（可选）</span><input value={preconditions} onChange={(event) => setPreconditions(event.target.value)} /></label>
          <label className="field field--full"><span>测试步骤（每行一条）</span><textarea value={stepsText} onChange={(event) => setStepsText(event.target.value)} rows={5} placeholder={'第一步操作\n第二步操作'} required /></label>
          <label className="field field--full"><span>预期结果</span><input value={expected} onChange={(event) => setExpected(event.target.value)} placeholder="可判定的预期表现" required /></label>
        </div>
      </div>
      <footer className="modal__footer">
        <button className="button" type="button" onClick={onClose}>取消</button>
        <button className="button button--primary" type="button" disabled={saving || !title.trim() || !stepsText.trim() || !expected.trim()} onClick={() => void submit()}>{saving ? '保存中…' : '创建用例'}</button>
      </footer>
    </Modal>
  )
}
