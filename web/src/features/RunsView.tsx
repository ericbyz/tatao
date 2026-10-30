import { useEffect, useMemo, useState } from 'react'
import { createManualTestCase, getRun, listRuns } from '../app/api'
import type { AgentConfig, Environment, Project, TestCase, TestCaseInput, TestRun } from '../app/types'
import { Icon } from '../components/Icon'
import { ExecuteRunDialog } from './ExecuteRunDialog'
import { Modal } from '../components/Modal'
import { StatusTag } from '../components/StatusTag'

type RunsViewProps = {
  projects: Project[]
  environments: Environment[]
  agents: AgentConfig[]
  testCases: TestCase[]
  onCasesChanged: () => Promise<void>
  notify: (message: string, tone?: 'success' | 'error') => void
}

type RunOutcome = 'passed' | 'failed' | 'error'

export function RunsView({ projects, environments, agents, testCases, onCasesChanged, notify }: RunsViewProps) {
  const [runs, setRuns] = useState<TestRun[]>([])
  const [projectId, setProjectId] = useState(projects[0]?.id ?? '')
  const [detail, setDetail] = useState<TestRun | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const projectNames = useMemo(() => new Map(projects.map((project) => [project.id, project.name])), [projects])

  const refreshRuns = async () => {
    try {
      setRuns((await listRuns()) ?? [])
    } catch (error) {
      notify(error instanceof Error ? error.message : '读取执行记录失败', 'error')
    }
  }

  useEffect(() => {
    void refreshRuns()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 详情页在执行中时轮询刷新
  useEffect(() => {
    if (!detail || detail.status !== 'running') return
    const timer = window.setInterval(async () => {
      try {
        const latest = await getRun(detail.id)
        setDetail(latest)
        setRuns((current) => current.map((run) => (run.id === latest.id ? latest : run)))
        if (latest.status !== 'running') {
          notify('执行批次已结束')
        }
      } catch {
        // 轮询失败静默，下一轮重试
      }
    }, 4000)
    return () => window.clearInterval(timer)
  }, [detail, notify])

  const openDetail = async (run: TestRun) => {
    setDetail(run)
    if (run.status === 'running') {
      try {
        setDetail(await getRun(run.id))
      } catch {
        // 保留列表数据
      }
    }
  }

  return (
    <main className="page-content cases-page">
      <div className="cases-toolbar">
        <div className="tabs" role="tablist">
          <button className="is-active" type="button">执行记录{runs.length > 0 ? ` · ${runs.length}` : ''}</button>
        </div>
        <div className="cases-toolbar__actions">
          <select className="toolbar-select" value={projectId} onChange={(event) => setProjectId(event.target.value)} aria-label="项目">
            {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
          </select>
          <button className="button button--small" type="button" onClick={() => void refreshRuns()}><Icon name="refresh" size={16} /> 刷新</button>
          <button className="button button--small button--primary" type="button" disabled={!projects.length} onClick={() => setCreateOpen(true)}>
            <Icon name="terminal" size={16} /> 一键测试
          </button>
        </div>
      </div>

      <div className="table-shell">
        <table className="data-table">
          <thead><tr><th>创建时间</th><th>项目</th><th>环境 / 模型</th><th>智能体</th><th>进度</th><th>当前执行</th><th>状态</th><th aria-label="操作" /></tr></thead>
          <tbody>
            {runs.map((run) => (
              <tr key={run.id} className="is-clickable" onClick={() => void openDetail(run)}>
                <td>{formatTime(run.createdAt)}</td>
                <td>{projectNames.get(run.projectId) ?? '未知项目'}</td>
                <td><span className="run-env">{run.environmentName}{run.model ? ` · ${run.model}` : ''}</span></td>
                <td>{run.agentName}</td>
                <td><span className="run-progress"><span className="run-progress__pass">{run.passed} 通过</span> / <span className="run-progress__fail">{run.failed} 失败</span> / {run.total} 总</span></td>
                <td className="run-current">{run.status === 'running' ? run.currentTitle || '准备中…' : '—'}</td>
                <td><RunStatusTag status={run.status} /></td>
                <td><button className="row-action" type="button" aria-label="查看详情" onClick={(event) => { event.stopPropagation(); void openDetail(run) }}><Icon name="dots" size={18} /></button></td>
              </tr>
            ))}
          </tbody>
        </table>
        {runs.length === 0 ? <div className="empty-inline">还没有执行记录。点「一键测试」选择用例交由智能体实际执行。</div> : null}
      </div>

      {createOpen ? (
        <ExecuteRunDialog
          projects={projects}
          environments={environments}
          agents={agents}
          testCases={testCases}
          defaultProjectId={projectId}
          onClose={() => setCreateOpen(false)}
          onCreated={(run) => {
            setRuns((current) => [run, ...current])
            setCreateOpen(false)
            notify('执行批次已开始，智能体正在逐条执行')
            void openDetail(run)
          }}
          notify={notify}
        />
      ) : null}

      {detail ? <RunDetailDialog run={detail} projectNames={projectNames} onClose={() => setDetail(null)} /> : null}
    </main>
  )
}

function RunStatusTag({ status }: { status: string }) {
  if (status === 'running') return <StatusTag tone="warning">执行中</StatusTag>
  return <StatusTag tone="success">已完成</StatusTag>
}

function ResultTag({ status }: { status: RunOutcome }) {
  if (status === 'passed') return <StatusTag tone="success">通过</StatusTag>
  if (status === 'failed') return <StatusTag tone="danger">失败</StatusTag>
  return <StatusTag tone="warning">错误</StatusTag>
}

function RunDetailDialog({ run, projectNames, onClose }: { run: TestRun; projectNames: Map<string, string>; onClose: () => void }) {
  const [expanded, setExpanded] = useState<string | null>(null)
  return (
    <Modal title={`执行批次 · ${run.status === 'running' ? '执行中' : '已完成'}`} description={`${projectNames.get(run.projectId) ?? ''} · 智能体 ${run.agentName} · 工作目录 ${run.workspaceDir}`} onClose={onClose} width="wide">
      <div className="run-detail">
        <div className="run-detail__summary">
          <div><span>通过</span><strong className="run-progress__pass">{run.passed}</strong></div>
          <div><span>失败</span><strong className="run-progress__fail">{run.failed}</strong></div>
          <div><span>错误</span><strong>{run.errors}</strong></div>
          <div><span>总数</span><strong>{run.total}</strong></div>
        </div>
        {run.status === 'running' ? <p className="notice-line" style={{ margin: '0 0 14px' }}><Icon name="refresh" size={15} /><span>正在执行：{run.currentTitle || '准备中…'}</span></p> : null}
        <div className="run-detail__results">
          {(run.results ?? []).map((result) => (
            <div key={result.caseId} className="run-result">
              <div className="run-result__head" onClick={() => setExpanded(expanded === result.caseId ? null : result.caseId)}>
                <ResultTag status={result.status} />
                <div>
                  <strong>{result.title}</strong>
                  <small>{result.source} · {(result.durationMs / 1000).toFixed(1)}s · {result.summary}</small>
                </div>
                <button className="row-action" type="button" aria-label="展开证据"><Icon name={expanded === result.caseId ? 'chevron' : 'dots'} size={16} /></button>
              </div>
              {expanded === result.caseId && result.evidence ? <pre className="run-result__evidence">{result.evidence}</pre> : null}
            </div>
          ))}
          {(run.results ?? []).length === 0 ? <div className="empty-inline">暂无结果，智能体正在执行第一条…</div> : null}
        </div>
      </div>
    </Modal>
  )
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

export type { TestCaseInput }
