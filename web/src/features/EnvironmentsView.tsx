import { useEffect, useState, type FormEvent } from 'react'
import { exploreEnvironmentKnowledge, listEnvironmentKnowledge } from '../app/api'
import type { Environment, KnowledgeEntry, Project } from '../app/types'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { StatusTag } from '../components/StatusTag'

type EnvironmentsViewProps = {
  projects: Project[]
  environments: Environment[]
  agents: { id: string; name: string; type: string; isDefault?: boolean }[]
  onCreate: (input: Omit<Environment, 'id' | 'projectIds' | 'os' | 'secretSet' | 'status'> & { secret: string }) => Promise<void>
  onAssign: (environmentId: string, projectIds: string[]) => Promise<void>
  notify: (message: string, tone?: 'success' | 'error') => void
}

export function EnvironmentsView({ projects, environments, agents, onCreate, onAssign, notify }: EnvironmentsViewProps) {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [assigningEnvironment, setAssigningEnvironment] = useState<Environment | null>(null)
  const [knowledgeEnv, setKnowledgeEnv] = useState<Environment | null>(null)
  const projectNames = new Map(projects.map((project) => [project.id, project.name]))

  return (
    <main className="page-content page-content--wide">
      <header className="page-header">
        <div><h1>测试环境</h1><p>管理运行测试所需的 Linux 主机和认证信息。</p></div>
        <button className="button button--primary" type="button" onClick={() => setDialogOpen(true)}><Icon name="plus" size={17}/> 新增环境</button>
      </header>
      <div className="notice-line"><Icon name="lock" size={17}/><span>密码和私钥只发送到后端密钥仓库，不会在列表、日志或接口响应中返回。</span></div>
      <div className="table-shell">
        <table className="data-table">
          <thead><tr><th>环境名称</th><th>已分配项目</th><th>系统</th><th>地址</th><th>账号</th><th>认证</th><th>状态</th><th>知识库</th><th aria-label="操作"/></tr></thead>
          <tbody>{environments.map((environment) => (
            <tr key={environment.id}>
              <td><span className="name-cell"><Icon name="server" size={19}/>{environment.name}</span></td>
              <td>{environment.projectIds.length ? environment.projectIds.map((id) => projectNames.get(id) ?? '未知项目').join('、') : <span className="muted-text">未分配</span>}</td>
              <td>Linux</td>
              <td className="mono">{environment.host}:{environment.port}</td>
              <td>{environment.username}</td>
              <td>{environment.authType === 'ssh_key' ? 'SSH Key' : '密码'}</td>
              <td><StatusTag tone={environment.status === 'online' ? 'success' : 'neutral'}>{environment.status === 'online' ? '在线' : '未检查'}</StatusTag></td>
              <td><button className="button button--small" type="button" onClick={() => setKnowledgeEnv(environment)}><Icon name="search" size={14}/> AI 探索</button></td>
              <td><button className="button button--small" type="button" onClick={() => setAssigningEnvironment(environment)}>分配项目</button></td>
            </tr>
          ))}</tbody>
        </table>
        {environments.length === 0 ? <div className="empty-inline">还没有测试环境。新增后可再分配给任意项目。</div> : null}
      </div>
      {dialogOpen ? <EnvironmentDialog onClose={() => setDialogOpen(false)} onCreate={async (input) => { await onCreate(input); setDialogOpen(false) }} /> : null}
      {assigningEnvironment ? <AssignmentDialog environment={assigningEnvironment} projects={projects} onClose={() => setAssigningEnvironment(null)} onAssign={async (projectIds) => { await onAssign(assigningEnvironment.id, projectIds); setAssigningEnvironment(null) }} /> : null}
      {knowledgeEnv ? <EnvironmentKnowledgeDialog environment={knowledgeEnv} agents={agents} onClose={() => setKnowledgeEnv(null)} notify={notify} /> : null}
    </main>
  )
}

function EnvironmentDialog({ onClose, onCreate }: { onClose: () => void; onCreate: EnvironmentsViewProps['onCreate'] }) {
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState(22)
  const [username, setUsername] = useState('')
  const [authType, setAuthType] = useState<Environment['authType']>('password')
  const [notes, setNotes] = useState('')
  const [secret, setSecret] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    try {
      await onCreate({ name, host, port, username, authType, notes: notes.trim() || undefined, secret })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title="新增 Linux 环境" description="保存后可继续执行连通性检查。" onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-stack form-grid">
          <label className="field field--full"><span>环境名称</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：预发布环境" required/></label>
          <label className="field"><span>主机地址</span><input value={host} onChange={(event) => setHost(event.target.value)} placeholder="10.0.0.8" required/></label>
          <label className="field"><span>SSH 端口</span><input type="number" value={port} onChange={(event) => setPort(Number(event.target.value))} min="1" max="65535" required/></label>
          <label className="field"><span>账号</span><input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="tester" required/></label>
          <label className="field"><span>认证方式</span><select value={authType} onChange={(event) => setAuthType(event.target.value as Environment['authType'])}><option value="password">密码</option><option value="ssh_key">SSH 私钥</option></select></label>
          <label className="field field--full"><span>备注（服务地址 / 注意事项，执行时提供给智能体）</span><input value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="例如：应用端口 8080，管理端 /admin，测试账号 admin/test123" /></label>
          <label className="field field--full"><span>{authType === 'password' ? '密码' : 'SSH 私钥'}</span>{authType === 'password' ? <input type="password" value={secret} onChange={(event) => setSecret(event.target.value)} autoComplete="new-password" required/> : <textarea value={secret} onChange={(event) => setSecret(event.target.value)} rows={4} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" required/>}</label>
        </div>
        <footer className="modal__footer"><button className="button" type="button" onClick={onClose}>取消</button><button className="button button--primary" type="submit" disabled={submitting}>{submitting ? '保存中…' : '保存环境'}</button></footer>
      </form>
    </Modal>
  )
}

function AssignmentDialog({ environment, projects, onClose, onAssign }: { environment: Environment; projects: Project[]; onClose: () => void; onAssign: (projectIds: string[]) => Promise<void> }) {
  const [selectedProjectIds, setSelectedProjectIds] = useState<string[]>(() => environment.projectIds)
  const [submitting, setSubmitting] = useState(false)

  const toggleProject = (projectId: string) => {
    setSelectedProjectIds((current) => current.includes(projectId) ? current.filter((id) => id !== projectId) : [...current, projectId])
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    try {
      await onAssign(selectedProjectIds)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title="分配项目" description={`选择可以使用“${environment.name}”的项目，可多选或暂不分配。`} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="assignment-list">
          {projects.map((project) => (
            <label key={project.id} className="assignment-option">
              <input type="checkbox" checked={selectedProjectIds.includes(project.id)} onChange={() => toggleProject(project.id)} />
              <span><strong>{project.name}</strong><small>{project.source === 'local' ? '本地目录' : 'TAPD'}</small></span>
            </label>
          ))}
          {projects.length === 0 ? <p className="assignment-empty">还没有可分配的项目。</p> : null}
        </div>
        <footer className="modal__footer"><button className="button" type="button" onClick={onClose}>取消</button><button className="button button--primary" type="submit" disabled={submitting}>{submitting ? '保存中…' : '保存分配'}</button></footer>
      </form>
    </Modal>
  )
}


// EnvironmentKnowledgeDialog 展示环境知识条目，一键 AI 探索自动沉淀。
function EnvironmentKnowledgeDialog({ environment, agents, onClose, notify }: {
  environment: Environment
  agents: { id: string; name: string; type: string; isDefault?: boolean }[]
  onClose: () => void
  notify: (message: string, tone?: 'success' | 'error') => void
}) {
  const [entries, setEntries] = useState<KnowledgeEntry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [exploring, setExploring] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)

  useEffect(() => {
    listEnvironmentKnowledge(environment.id).then(setEntries).catch(() => setEntries([]))
  }, [environment.id])

  const explore = async () => {
    setExploring(true)
    try {
      const result = await exploreEnvironmentKnowledge(environment.id)
      setEntries((current) => [...(result.entries ?? []), ...current])
      notify('AI 探索完成：新增 ' + (result.count ?? 0) + ' 条环境知识')
    } catch (error) {
      notify(error instanceof Error ? error.message : 'AI 探索失败', 'error')
    } finally {
      setExploring(false)
    }
  }

  return (
    <Modal title={'环境知识库 · '+environment.name} description="AI 只读探索环境（部署的服务、端口、配置、风险），沉淀为知识供用例设计与执行引用。" onClose={onClose} width="wide">
      <div className="kb-head">
        <h3 style={{ fontSize: 13 }}>知识条目（{entries.length}）</h3>
        <button className="button button--small button--primary" type="button" disabled={exploring} onClick={() => void explore()}>
          <Icon name="search" size={14} /> {exploring ? 'AI 探索中…（1-3 分钟）' : 'AI 探索环境'}
        </button>
      </div>
      <div className="kb-list">
        {entries.map((entry) => (
          <div key={entry.id} className="kb-entry">
            <div className="kb-entry__head" onClick={() => setExpanded(expanded === entry.id ? null : entry.id)}>
              <span className="kb-entry__cat">{entry.category}</span>
              <strong>{entry.title}</strong>
              <span className={entry.source === 'ai' ? 'kb-source kb-source--ai' : 'kb-source'}>{entry.source === 'ai' ? 'AI' : '手动'}</span>
              <div className="kb-entry__actions">
                <button className="row-action" type="button" aria-label="展开" onClick={(event) => { event.stopPropagation(); setExpanded(expanded === entry.id ? null : entry.id) }}><Icon name={expanded === entry.id ? 'chevron' : 'dots'} size={15} /></button>
              </div>
            </div>
            {expanded === entry.id ? <p className="kb-entry__content">{entry.content}</p> : null}
          </div>
        ))}
        {entries.length === 0 ? <div className="empty-inline">{loaded || entries.length === 0 ? '点击「AI 探索环境」让智能体自动探索并沉淀知识。' : ''}</div> : null}
      </div>
    </Modal>
  )
}
