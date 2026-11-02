import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { deleteKnowledge, listKnowledge, listTAPDWorkspaces, saveKnowledge, updateProject } from '../app/api'
import type { KnowledgeEntry } from '../app/types'
import type { Project, TAPDWorkspace } from '../app/types'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { StatusTag } from '../components/StatusTag'

type ProjectsViewProps = {
  projects: Project[]
  selectedId: string
  onSelect: (id: string) => void
  onCreate: (project: Pick<Project, 'name' | 'source'> & Partial<Project>) => Promise<void>
  onSync: (project: Project) => Promise<void>
  notify: (message: string, tone?: 'success' | 'error') => void
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

export function ProjectsView({ projects, selectedId, onSelect, onCreate, onSync, notify }: ProjectsViewProps) {
  const [query, setQuery] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const selectedProject = projects.find((project) => project.id === selectedId) ?? projects[0]
  const filteredProjects = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    return normalized ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalized)) : projects
  }, [projects, query])

  return (
    <div className={`projects-layout ${selectedProject ? '' : 'projects-layout--empty'}`}>
      <main className="page-content">
        <header className="page-header">
          <div>
            <h1>项目</h1>
          </div>
          <div className="page-actions">
            <label className="search-field">
              <Icon name="search" size={18} />
              <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索项目" />
            </label>
            <button className="button button--primary" type="button" onClick={() => setDialogOpen(true)}>
              <Icon name="plus" size={17} /> 新建项目
            </button>
          </div>
        </header>

        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>项目名称</th>
                <th>来源</th>
                <th>测试环境</th>
                <th>测试用例</th>
                <th>最近更新</th>
                <th>状态</th>
                <th aria-label="操作" />
              </tr>
            </thead>
            <tbody>
              {filteredProjects.map((project) => (
                <tr
                  key={project.id}
                  className={project.id === selectedProject?.id ? 'is-selected' : undefined}
                  onClick={() => onSelect(project.id)}
                >
                  <td><span className="name-cell"><Icon name="folder" size={19} />{project.name}</span></td>
                  <td>{project.source === 'local' ? '本地目录' : 'TAPD'}</td>
                  <td>{project.environmentCount ? `Linux · ${project.environmentCount}` : '—'}</td>
                  <td>{project.caseCount}</td>
                  <td>{formatDate(project.updatedAt)}</td>
                  <td>
                    <StatusTag tone={project.status === 'ready' ? 'success' : 'warning'}>
                      {project.status === 'ready' ? '可用' : '待配置'}
                    </StatusTag>
                  </td>
                  <td><button className="row-action" type="button" aria-label={`${project.name}更多操作`}><Icon name="dots" size={18}/></button></td>
                </tr>
              ))}
            </tbody>
          </table>
          {filteredProjects.length === 0 ? <div className="empty-inline">{query.trim() ? '没有匹配的项目' : '还没有项目，点击“新建项目”开始。'}</div> : null}
        </div>
      </main>

      {selectedProject ? (
        <aside className="detail-panel">
          <div className="detail-title">
            <span className="detail-icon"><Icon name="folder" size={24} /></span>
            <h2>{selectedProject.name}</h2>
            <button className="icon-button" type="button" aria-label="更多操作"><Icon name="dots" size={19} /></button>
          </div>
          <div className="detail-actions">
            <button className="button button--primary" type="button" onClick={() => notify('项目工作区已准备好')}>
              <Icon name="folder" size={17} /> 打开项目
            </button>
            <button className="button" type="button" disabled={selectedProject.source !== 'tapd'} onClick={() => onSync(selectedProject)}>
              <Icon name="refresh" size={17} /> 同步 TAPD
            </button>
          </div>
          <ContextSection project={selectedProject} notify={notify} />

          <KnowledgeSection projectId={selectedProject.id} notify={notify} />

          <section className="detail-section">
            <h3>项目信息</h3>
            <dl className="detail-list">
              <div><dt>来源</dt><dd>{selectedProject.source === 'local' ? '本地目录' : 'TAPD'}</dd></div>
              <div><dt>测试环境</dt><dd>Linux · {selectedProject.environmentCount}</dd></div>
              <div><dt>测试用例</dt><dd>{selectedProject.caseCount}</dd></div>
              <div><dt>最近更新</dt><dd>{formatDate(selectedProject.updatedAt)}</dd></div>
              <div><dt>状态</dt><dd><StatusTag tone={selectedProject.status === 'ready' ? 'success' : 'warning'}>{selectedProject.status === 'ready' ? '可用' : '待配置'}</StatusTag></dd></div>
            </dl>
          </section>
          <section className="detail-section">
            <h3>最近动态</h3>
            <p className="detail-empty">暂无动态</p>
          </section>
        </aside>
      ) : null}

      {dialogOpen ? <CreateProjectDialog onClose={() => setDialogOpen(false)} onCreate={async (input) => {
        await onCreate(input)
        setDialogOpen(false)
      }} /> : null}
    </div>
  )
}

function CreateProjectDialog({ onClose, onCreate }: { onClose: () => void; onCreate: ProjectsViewProps['onCreate'] }) {
  const [source, setSource] = useState<Project['source']>('local')
  const [name, setName] = useState('')
  const [localPath, setLocalPath] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')
  const [workspaces, setWorkspaces] = useState<TAPDWorkspace[]>([])
  const [workspaceError, setWorkspaceError] = useState('')
  const [loadingWorkspaces, setLoadingWorkspaces] = useState(false)
  const [step, setStep] = useState<1 | 2>(1)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (step !== 2 || source !== 'tapd' || workspaces.length > 0) return
    const controller = new AbortController()
    setLoadingWorkspaces(true)
    setWorkspaceError('')
    listTAPDWorkspaces(controller.signal)
      .then((result) => {
        setWorkspaces(result)
        if (result.length > 0) setWorkspaceId((current) => current || result[0].id)
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') return
        setWorkspaceError(error instanceof Error ? error.message : '获取工作区失败')
      })
      .finally(() => setLoadingWorkspaces(false))
    return () => controller.abort()
  }, [step, source, workspaces.length])

  const chooseDirectory = async () => {
    const picker = (window as unknown as { showDirectoryPicker?: () => Promise<{ name: string }> }).showDirectoryPicker
    if (!picker) return
    try {
      const handle = await picker()
      setLocalPath(`已授权 / ${handle.name}`)
      if (!name) setName(handle.name)
    } catch {
      // The user closing the native picker is not an application error.
    }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) return
    setSubmitting(true)
    try {
      await onCreate({
        name: name.trim(),
        source,
        localPath: source === 'local' ? localPath.trim() : undefined,
        tapdWorkspaceId: source === 'tapd' ? workspaceId.trim() : undefined,
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title="新建项目" description={step === 1 ? '选择项目来源，将自动初始化项目配置。' : '填写基础信息，稍后仍可在项目设置中修改。'} onClose={onClose} width="wide">
      {step === 1 ? (
        <>
          <div className="source-grid">
            <button className={`source-option ${source === 'local' ? 'is-active' : ''}`} type="button" onClick={() => setSource('local')}>
              <span className="source-option__radio">{source === 'local' ? <span /> : null}</span>
              <span className="source-option__icon"><Icon name="folder" size={28} /></span>
              <strong>本地文件夹</strong>
              <small>通过本地代码或测试脚本目录创建项目</small>
            </button>
            <button className={`source-option ${source === 'tapd' ? 'is-active' : ''}`} type="button" onClick={() => setSource('tapd')}>
              <span className="source-option__radio">{source === 'tapd' ? <span /> : null}</span>
              <span className="source-option__icon source-option__icon--tapd">T</span>
              <strong>连接 TAPD</strong>
              <small>从 TAPD 同步需求、缺陷等数据创建项目</small>
            </button>
          </div>
          <footer className="modal__footer">
            <button className="button" type="button" onClick={onClose}>取消</button>
            <button className="button button--primary" type="button" onClick={() => setStep(2)}>下一步</button>
          </footer>
        </>
      ) : (
        <form onSubmit={submit}>
          <div className="form-stack">
            <label className="field"><span>项目名称</span><input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：支付网关" required /></label>
            {source === 'local' ? (
              <label className="field"><span>本地文件夹</span><div className="input-action"><input value={localPath} onChange={(event) => setLocalPath(event.target.value)} placeholder="选择或输入项目目录" /><button type="button" onClick={chooseDirectory}>选择文件夹</button></div><small>浏览器只会记录已授权的目录名称，不会上传目录内容。</small></label>
            ) : (
              <label className="field">
                <span>TAPD 工作区</span>
                {workspaceError ? (
                  <div className="notice-line"><Icon name="lock" size={16} /><span>{workspaceError}</span></div>
                ) : loadingWorkspaces ? (
                  <select disabled><option>正在加载工作区…</option></select>
                ) : (
                  <select value={workspaceId} onChange={(event) => setWorkspaceId(event.target.value)} disabled={!workspaces.length}>
                    {workspaces.length === 0 ? <option value="">没有可见的工作区</option> : workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}（{workspace.id}）</option>)}
                  </select>
                )}
                {workspaceError.includes('公司 ID') ? (
                  <>
                    <div className="input-action"><input value={workspaceId} onChange={(event) => setWorkspaceId(event.target.value)} placeholder="手动填写 TAPD 工作区 ID，例如 44165834" /></div>
                    <small>公司 ID 可在 TAPD 网页地址栏查看（tapd.cn 后的数字）；填写后到「连接设置」保存，即可显示全部项目。</small>
                  </>
                ) : (
                  <small>工作区列表来自当前配置的 TAPD 连接。</small>
                )}
              </label>
            )}
          </div>
          <footer className="modal__footer">
            <button className="button" type="button" onClick={() => setStep(1)}>上一步</button>
            <button className="button button--primary" type="submit" disabled={!name.trim() || !workspaceId.trim() || submitting}>{submitting ? '创建中…' : '创建项目'}</button>
          </footer>
        </form>
      )}
    </Modal>
  )
}


// ContextSection 编辑项目上下文说明：这是执行时喂给智能体的被测系统背景。
function ContextSection({ project, notify }: { project: Project; notify: (message: string, tone?: 'success' | 'error') => void }) {
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(project.context ?? '')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setEditing(false)
    setText(project.context ?? '')
  }, [project.id, project.context])

  const save = async () => {
    setSaving(true)
    try {
      await updateProject(project.id, { context: text })
      notify('项目上下文已保存，执行用例时会提供给智能体')
      setEditing(false)
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存失败', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="detail-section">
      <h3>项目上下文</h3>
      {editing ? (
        <div className="form-stack">
          <textarea
            className="context-editor"
            value={text}
            onChange={(event) => setText(event.target.value)}
            rows={6}
            placeholder={'被测系统是什么、技术栈、如何启动与运行测试、注意事项…\n执行与设计用例时会作为背景提供给智能体。'}
          />
          <div className="context-actions">
            <button className="button button--small" type="button" onClick={() => { setEditing(false); setText(project.context ?? '') }}>取消</button>
            <button className="button button--small button--primary" type="button" disabled={saving} onClick={() => void save()}>{saving ? '保存中…' : '保存'}</button>
          </div>
        </div>
      ) : (
        <div>
          <p className="context-text">{project.context ? project.context : '尚未填写。描述被测系统、技术栈、启动/测试命令，执行时会提供给智能体。'}</p>
          <button className="button button--small" type="button" onClick={() => setEditing(true)}><Icon name="clipboard" size={14} /> 编辑上下文</button>
        </div>
      )}
    </section>
  )
}

const knowledgeCategories = ['功能导航', '数据与文件约定', '接口与服务', '账号与凭据', '术语解释', '禁忌与风险']

// KnowledgeSection 维护项目知识库：设计/执行用例时这些条目会注入智能体提示词。
function KnowledgeSection({ projectId, notify }: { projectId: string; notify: (message: string, tone?: 'success' | 'error') => void }) {
  const [entries, setEntries] = useState<KnowledgeEntry[]>([])
  const [editing, setEditing] = useState<KnowledgeEntry | 'new' | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)

  useEffect(() => {
    setEntries([])
    setEditing(null)
    listKnowledge(projectId).then(setEntries).catch(() => setEntries([]))
  }, [projectId])

  const remove = async (entry: KnowledgeEntry) => {
    try {
      await deleteKnowledge(entry.id)
      setEntries((current) => current.filter((item) => item.id !== entry.id))
    } catch (error) {
      notify(error instanceof Error ? error.message : '删除失败', 'error')
    }
  }

  return (
    <section className="detail-section">
      <div className="kb-head">
        <h3>项目知识库</h3>
        <button className="button button--small" type="button" onClick={() => setEditing('new')}><Icon name="plus" size={14} /> 新增</button>
      </div>
      {editing !== null ? (
        <KnowledgeEditDialog
          projectId={projectId}
          entry={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(saved) => {
            setEntries((current) => {
              const next = current.filter((item) => item.id !== saved.id)
              return [saved, ...next]
            })
            setEditing(null)
          }}
          notify={notify}
        />
      ) : null}
      <div className="kb-list">
        {entries.map((entry) => (
          <div key={entry.id} className="kb-entry">
            <div className="kb-entry__head" onClick={() => setExpanded(expanded === entry.id ? null : entry.id)}>
              <span className="kb-entry__cat">{entry.category}</span>
              <strong>{entry.title}</strong>
              <div className="kb-entry__actions">
                <button className="row-action" type="button" aria-label="编辑" onClick={(event) => { event.stopPropagation(); setEditing(entry) }}><Icon name="dots" size={15} /></button>
                <button className="row-action" type="button" aria-label="删除" onClick={(event) => { event.stopPropagation(); void remove(entry) }}><Icon name="close" size={15} /></button>
              </div>
            </div>
            {expanded === entry.id ? <p className="kb-entry__content">{entry.content}</p> : null}
          </div>
        ))}
        {entries.length === 0 ? <p className="detail-empty">还没有知识条目。录入功能入口、数据约定、账号、禁忌等，智能体设计与执行用例时会自动引用。</p> : null}
      </div>
    </section>
  )
}

function KnowledgeEditDialog({ projectId, entry, onClose, onSaved, notify }: {
  projectId: string
  entry: KnowledgeEntry | null
  onClose: () => void
  onSaved: (saved: KnowledgeEntry) => void
  notify: (message: string, tone?: 'success' | 'error') => void
}) {
  const [category, setCategory] = useState(entry?.category ?? knowledgeCategories[0])
  const [title, setTitle] = useState(entry?.title ?? '')
  const [content, setContent] = useState(entry?.content ?? '')
  const [saving, setSaving] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    try {
      const saved = await saveKnowledge(projectId, { id: entry?.id, category, title: title.trim(), content: content.trim() })
      onSaved(saved)
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存失败', 'error')
      setSaving(false)
    }
  }

  return (
    <Modal title={entry ? '编辑知识条目' : '新增知识条目'} description="这些知识会在设计与执行用例时提供给智能体。" onClose={onClose} width="wide">
      <form onSubmit={submit}>
        <div className="form-stack">
          <div className="form-grid">
            <label className="field"><span>类别</span>
              <select value={category} onChange={(event) => setCategory(event.target.value)}>
                {knowledgeCategories.map((value) => <option key={value} value={value}>{value}</option>)}
              </select>
            </label>
            <label className="field"><span>标题</span><input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="例如：创建同步任务的步骤" required /></label>
            <label className="field field--full"><span>内容</span><textarea value={content} onChange={(event) => setContent(event.target.value)} rows={6} placeholder={'例如：入口在「任务管理」→「新建任务」；需要选择源目录与目标目录；配置保存在 config/tasks.json…'} required /></label>
          </div>
        </div>
        <footer className="modal__footer">
          <button className="button" type="button" onClick={onClose}>取消</button>
          <button className="button button--primary" type="submit" disabled={saving}>{saving ? '保存中…' : '保存条目'}</button>
        </footer>
      </form>
    </Modal>
  )
}
