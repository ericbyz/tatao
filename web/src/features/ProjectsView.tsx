import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { listTAPDWorkspaces } from '../app/api'
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

