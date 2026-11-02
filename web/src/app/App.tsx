import { useCallback, useEffect, useRef, useState } from 'react'
import {
  assignEnvironment,
  createEnvironment,
  createProject,
  deleteAgent as deleteAgentApi,
  generateTestCases,
  loadExternalItems,
  loadWorkspace,
  saveAgent as saveAgentApi,
  saveTAPDSettings,
  syncTAPD,
  testAgent as testAgentApi,
  testTAPDSettings,
} from './api'
import type { AgentConfig, AgentInput, Environment, ExternalItem, PageKey, Project, TAPDSettings, TAPDSettingsInput, TAPDStatus, TestCase } from './types'
import { Icon, type IconName } from '../components/Icon'
import { EnvironmentsView } from '../features/EnvironmentsView'
import { ProjectsView } from '../features/ProjectsView'
import { ConnectionsView, HomeView } from '../features/SecondaryViews'
import { RunsView } from '../features/RunsView'
import { TestCasesView } from '../features/TestCasesView'

const navigation: { id: PageKey; label: string; icon: IconName }[] = [
  { id: 'home', label: '工作台', icon: 'home' },
  { id: 'projects', label: '项目', icon: 'folder' },
  { id: 'environments', label: '测试环境', icon: 'server' },
  { id: 'cases', label: '测试用例', icon: 'clipboard' },
  { id: 'runs', label: '执行记录', icon: 'clock' },
  { id: 'connections', label: '连接设置', icon: 'settings' },
]

type Toast = { id: number; message: string; tone: 'success' | 'error' }

export default function App() {
  const [page, setPage] = useState<PageKey>('projects')
  const [projects, setProjects] = useState<Project[]>([])
  const [environments, setEnvironments] = useState<Environment[]>([])
  const [testCases, setTestCases] = useState<TestCase[]>([])
  const [externalItems, setExternalItems] = useState<ExternalItem[]>([])
  const [tapdStatus, setTAPDStatus] = useState<TAPDStatus | null>(null)
  const [tapdSettings, setTAPDSettings] = useState<TAPDSettings | null>(null)
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [selectedProjectId, setSelectedProjectId] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const selectedProjectRef = useRef('')
  const projectsWithEnvironmentCounts = projects.map((project) => ({
    ...project,
    environmentCount: environments.filter((environment) => environment.projectIds.includes(project.id)).length,
  }))

  const notify = useCallback((message: string, tone: Toast['tone'] = 'success') => {
    const id = Date.now()
    setToasts((current) => [...current, { id, message, tone }])
    window.setTimeout(() => setToasts((current) => current.filter((toast) => toast.id !== id)), 3200)
  }, [])

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setLoadError('')
    try {
      const [projectData, environmentData, testCaseData, connectorStatus, connectorSettings, agentData] = await loadWorkspace(signal)
      setProjects(projectData)
      setEnvironments(environmentData)
      setTestCases(testCaseData)
      setTAPDStatus(connectorStatus)
      setTAPDSettings(connectorSettings)
      setAgents(agentData ?? [])
      const targetProjectId = selectedProjectRef.current || projectData[0]?.id || ''
      selectedProjectRef.current = targetProjectId
      setSelectedProjectId(targetProjectId)
      try {
        setExternalItems(await loadExternalItems(undefined, signal))
      } catch (error) {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setExternalItems([])
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      setLoadError(error instanceof Error ? error.message : '无法读取工作区')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void refresh(controller.signal)
    return () => controller.abort()
  }, [refresh])

  const refreshExternalItems = useCallback(async () => {
    try {
      setExternalItems(await loadExternalItems())
    } catch (error) {
      notify(error instanceof Error ? error.message : '读取 TAPD 素材失败', 'error')
    }
  }, [notify])

  const handleSelectProject = useCallback((projectId: string) => {
    selectedProjectRef.current = projectId
    setSelectedProjectId(projectId)
  }, [])

  const handleCreateProject = async (input: Pick<Project, 'name' | 'source'> & Partial<Project>) => {
    try {
      const project = await createProject(input)
      setProjects((current) => [project, ...current])
      setSelectedProjectId(project.id)
      notify('项目已创建')
    } catch (error) {
      notify(error instanceof Error ? error.message : '创建项目失败', 'error')
      throw error
    }
  }

  const handleCreateEnvironment = async (input: Omit<Environment, 'id' | 'projectIds' | 'os' | 'secretSet' | 'status'> & { secret: string }) => {
    try {
      const environment = await createEnvironment(input)
      setEnvironments((current) => [environment, ...current])
      notify('Linux 环境已保存')
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存环境失败', 'error')
      throw error
    }
  }

  const handleAssignEnvironment = async (environmentId: string, projectIds: string[]) => {
    try {
      const environment = await assignEnvironment(environmentId, projectIds)
      setEnvironments((current) => current.map((item) => item.id === environment.id ? environment : item))
      notify('项目分配已更新')
    } catch (error) {
      notify(error instanceof Error ? error.message : '分配项目失败', 'error')
      throw error
    }
  }

  const handleSync = async (project: Project) => {
    try {
      const summary = await syncTAPD(project.id)
      notify(`TAPD 同步完成：缺陷 ${summary.bugs} 条，需求 ${summary.requirements} 条`)
      await refreshExternalItems()
      setPage('cases')
    } catch (error) {
      notify(error instanceof Error ? error.message : 'TAPD 同步失败', 'error')
    }
  }

  const handleGenerate = async (projectId: string, itemIds: string[], agentId?: string, model?: string) => {
    try {
      const created = await generateTestCases(projectId, itemIds, agentId, model)
      setTestCases((current) => [...created, ...current])
      notify(`已生成 ${created.length} 条草稿用例`)
    } catch (error) {
      notify(error instanceof Error ? error.message : '生成用例失败', 'error')
      throw error
    }
  }

  const handleSaveTAPD = async (input: TAPDSettingsInput) => {
    const result = await saveTAPDSettings(input)
    setTAPDSettings(result.settings)
    setTAPDStatus(result.status)
    return result
  }

  const handleTestTAPD = async (input: TAPDSettingsInput) => {
    const status = await testTAPDSettings(input)
    setTAPDStatus(status)
    return status
  }

  const handleSaveAgent = async (input: AgentInput) => {
    const saved = await saveAgentApi(input)
    setAgents((current) => {
      const next = current.filter((agent) => agent.id !== saved.id)
      if (saved.isDefault) return [saved, ...next.map((agent) => ({ ...agent, isDefault: false }))]
      return [...next, saved]
    })
    return saved
  }

  const handleDeleteAgent = async (id: string) => {
    await deleteAgentApi(id)
    setAgents((current) => current.filter((agent) => agent.id !== id))
  }

  const handleTestAgent = async (input: AgentInput) => {
    const result = await testAgentApi(input)
    if (!result.ok) throw new Error(result.message)
    return result.message
  }

  const handleRunStarted = useCallback(() => {
    // 批次由后台执行，进入执行记录页时会自动拉取最新状态
  }, [])

  const renderPage = () => {
    if (loading) return <div className="loading-view"><span/><p>正在读取工作区…</p></div>
    if (loadError) return <div className="error-view"><strong>无法连接后端</strong><p>{loadError}</p><button className="button" type="button" onClick={() => refresh()}>重试</button></div>
    switch (page) {
      case 'home': return <HomeView projects={projectsWithEnvironmentCounts} />
      case 'projects': return <ProjectsView projects={projectsWithEnvironmentCounts} environments={environments} agents={agents} selectedId={selectedProjectId} onSelect={handleSelectProject} onCreate={handleCreateProject} onSync={handleSync} notify={notify} />
      case 'environments': return <EnvironmentsView projects={projectsWithEnvironmentCounts} environments={environments} onCreate={handleCreateEnvironment} onAssign={handleAssignEnvironment} />
      case 'cases': return <TestCasesView projects={projectsWithEnvironmentCounts} environments={environments} testCases={testCases} externalItems={externalItems} agents={agents} projectId={selectedProjectId} onProjectChange={handleSelectProject} onSync={handleSync} onGenerate={handleGenerate} onCasesChanged={refresh} onRunStarted={handleRunStarted} notify={notify} />
      case 'runs': return <RunsView environments={environments} projects={projectsWithEnvironmentCounts} agents={agents} testCases={testCases} onCasesChanged={refresh} notify={notify} />
      case 'connections': return <ConnectionsView settings={tapdSettings} status={tapdStatus} agents={agents} onSave={handleSaveTAPD} onTest={handleTestTAPD} onSaveAgent={handleSaveAgent} onDeleteAgent={handleDeleteAgent} onTestAgent={handleTestAgent} notify={notify} />
    }
  }

  return (
    <div className="app-shell">
      <aside className={`sidebar ${sidebarOpen ? 'is-open' : ''}`}>
        <div className="brand"><strong>AI Test Tool</strong></div>
        <nav aria-label="主导航">
          {navigation.map((item) => <button key={item.id} className={page === item.id ? 'is-active' : ''} type="button" onClick={() => { setPage(item.id); setSidebarOpen(false) }}><Icon name={item.icon} size={20}/><span>{item.label}</span></button>)}
        </nav>
      </aside>
      <header className="topbar">
        <button className="mobile-menu" type="button" aria-label="打开导航" onClick={() => setSidebarOpen((value) => !value)}><Icon name="menu" size={20}/></button>
        <span className="topbar__context">AI Test Tool</span>
        <div className="topbar__actions"><button className="icon-button" type="button" aria-label="搜索"><Icon name="search" size={19}/></button><button className="icon-button notification-button" type="button" aria-label="通知"><Icon name="bell" size={19}/><span/></button><div className="profile"><span>张</span><strong>张三</strong><Icon name="chevron" size={14}/></div></div>
      </header>
      <div className="workspace">{renderPage()}</div>
      {sidebarOpen ? <button className="sidebar-scrim" type="button" aria-label="关闭导航" onClick={() => setSidebarOpen(false)} /> : null}
      <div className="toast-stack" aria-live="polite">{toasts.map((toast) => <div key={toast.id} className={`toast toast--${toast.tone}`}><Icon name={toast.tone === 'success' ? 'check' : 'close'} size={17}/><span>{toast.message}</span></div>)}</div>
    </div>
  )
}
