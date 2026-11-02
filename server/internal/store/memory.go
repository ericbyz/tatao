package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"ai-test-tool/server/internal/domain"
)

var (
	ErrProjectNotFound     = errors.New("project not found")
	ErrEnvironmentNotFound = errors.New("environment not found")
)

// 密钥库固定键名。
const (
	SecretTAPD        = "tapd"
	SecretAgentPrefix = "agent:"
)

type Memory struct {
	mu            sync.RWMutex
	projects      []domain.Project
	environments  []domain.Environment
	testCases     []domain.TestCase
	externalItems map[string][]domain.ExternalItem
	tapdSettings  domain.TAPDSettings
	agents        []domain.AgentConfig
	runs          []domain.TestRun
	knowledge     []domain.KnowledgeEntry
	secrets       map[string][]byte
	aead          cipher.AEAD
	secretKey     []byte
	dataPath      string
}

func NewMemory() *Memory {
	key := randomKey()
	return &Memory{
		projects:      []domain.Project{},
		environments:  []domain.Environment{},
		testCases:     []domain.TestCase{},
		externalItems: make(map[string][]domain.ExternalItem),
		agents:        []domain.AgentConfig{},
		runs:          []domain.TestRun{},
		knowledge:     []domain.KnowledgeEntry{},
		secrets:       make(map[string][]byte),
		aead:          aeadFromKey(key),
		secretKey:     key,
	}
}

// EnablePersistence 开启 JSON 文件持久化：启动时加载数据与密钥，
// 之后每次变更落盘。密钥单独存放，重启后凭据仍可解密。
func (m *Memory) EnablePersistence(dataPath string) error {
	if strings.TrimSpace(dataPath) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	m.dataPath = dataPath
	if err := m.loadLocked(); err != nil {
		return err
	}
	return m.saveLocked()
}

type persistedState struct {
	Projects      []domain.Project                 `json:"projects"`
	Environments  []domain.Environment             `json:"environments"`
	TestCases     []domain.TestCase                `json:"testCases"`
	ExternalItems map[string][]domain.ExternalItem `json:"externalItems"`
	TAPDSettings  domain.TAPDSettings              `json:"tapdSettings"`
	Agents        []domain.AgentConfig             `json:"agents"`
	Runs          []domain.TestRun                 `json:"runs"`
	Knowledge     []domain.KnowledgeEntry          `json:"knowledge"`
	Secrets       map[string][]byte                `json:"secrets"`
}

func (m *Memory) loadLocked() error {
	content, err := os.ReadFile(m.dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(content, &state); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	m.projects = state.Projects
	m.environments = state.Environments
	m.testCases = state.TestCases
	m.externalItems = state.ExternalItems
	if m.externalItems == nil {
		m.externalItems = map[string][]domain.ExternalItem{}
	}
	m.tapdSettings = state.TAPDSettings
	m.agents = state.Agents
	m.runs = state.Runs
	m.knowledge = state.Knowledge
	m.secrets = state.Secrets
	if m.agents == nil {
		m.agents = []domain.AgentConfig{}
	}
	if m.runs == nil {
		m.runs = []domain.TestRun{}
	}
	if m.secrets == nil {
		m.secrets = map[string][]byte{}
	}
	if keyB64, err := os.ReadFile(m.keyPath()); err == nil {
		if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyB64))); err == nil && len(key) == 32 {
			m.secretKey = key
			m.aead = aeadFromKey(key)
		}
	}
	return nil
}

func (m *Memory) saveLocked() error {
	content, err := json.Marshal(persistedState{
		Projects:      m.projects,
		Environments:  m.environments,
		TestCases:     m.testCases,
		ExternalItems: m.externalItems,
		TAPDSettings:  m.tapdSettings,
		Agents:        m.agents,
		Runs:          m.runs,
		Knowledge:     m.knowledge,
		Secrets:       m.secrets,
	})
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(m.dataPath, content, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if _, err := os.Stat(m.keyPath()); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(m.keyPath(), []byte(base64.StdEncoding.EncodeToString(m.secretKey)), 0o600); err != nil {
			return fmt.Errorf("write secret key: %w", err)
		}
	}
	return nil
}

func (m *Memory) keyPath() string {
	return m.dataPath + ".key"
}

// persist 在每次变更后调用；持久化失败仅记录，不阻断业务操作。
func (m *Memory) persist() {
	if m.dataPath == "" {
		return
	}
	if err := m.saveLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "persist state: %v\n", err)
	}
}

func randomKey() []byte {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic(fmt.Errorf("create secret key: %w", err))
	}
	return key
}

func aeadFromKey(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

func (m *Memory) Projects() []domain.Project {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.projects)
}

func (m *Memory) AddProject(project domain.Project) domain.Project {
	m.mu.Lock()
	defer m.mu.Unlock()
	project.ID = newID("prj")
	project.UpdatedAt = time.Now()
	project.Status = "needs_setup"
	m.projects = append([]domain.Project{project}, m.projects...)
	m.persist()
	return project
}

func (m *Memory) Environments() []domain.Environment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.environments)
}

func (m *Memory) AddEnvironment(environment domain.Environment, secret string) (domain.Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	environment.ID = newID("env")
	environment.ProjectIDs = []string{}
	environment.OS = "linux"
	environment.Status = "unchecked"
	environment.SecretSet = secret != ""
	if secret != "" {
		m.secrets[environment.ID] = m.seal(environment.ID, secret)
	}

	m.environments = append([]domain.Environment{environment}, m.environments...)
	m.persist()
	return environment, nil
}

func (m *Memory) AssignEnvironment(environmentID string, projectIDs []string) (domain.Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	environmentIndex := slices.IndexFunc(m.environments, func(environment domain.Environment) bool {
		return environment.ID == environmentID
	})
	if environmentIndex < 0 {
		return domain.Environment{}, ErrEnvironmentNotFound
	}

	knownProjects := make(map[string]struct{}, len(m.projects))
	for _, project := range m.projects {
		knownProjects[project.ID] = struct{}{}
	}
	uniqueProjectIDs := make([]string, 0, len(projectIDs))
	seen := make(map[string]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		if _, ok := knownProjects[projectID]; !ok {
			return domain.Environment{}, ErrProjectNotFound
		}
		if _, ok := seen[projectID]; ok {
			continue
		}
		seen[projectID] = struct{}{}
		uniqueProjectIDs = append(uniqueProjectIDs, projectID)
	}

	m.environments[environmentIndex].ProjectIDs = uniqueProjectIDs
	for projectIndex := range m.projects {
		count := 0
		for _, environment := range m.environments {
			if slices.Contains(environment.ProjectIDs, m.projects[projectIndex].ID) {
				count++
			}
		}
		m.projects[projectIndex].EnvironmentCount = count
	}
	m.persist()
	return m.environments[environmentIndex], nil
}

func (m *Memory) TestCases() []domain.TestCase {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.testCases)
}

func (m *Memory) AddTestCase(testCase domain.TestCase) (domain.TestCase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	projectIndex := slices.IndexFunc(m.projects, func(project domain.Project) bool {
		return project.ID == testCase.ProjectID
	})
	if projectIndex < 0 {
		return domain.TestCase{}, ErrProjectNotFound
	}

	testCase.ID = newID("case")
	if testCase.Status == "" {
		testCase.Status = "draft"
	}
	m.testCases = append([]domain.TestCase{testCase}, m.testCases...)
	m.projects[projectIndex].CaseCount++
	m.projects[projectIndex].UpdatedAt = time.Now()
	m.persist()
	return testCase, nil
}

func newID(prefix string) string {
	value := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		panic(fmt.Errorf("create %s id: %w", prefix, err))
	}
	return prefix + "_" + hex.EncodeToString(value)
}

func (m *Memory) seal(id, value string) []byte {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(fmt.Errorf("seal secret %s: %w", id, err))
	}
	return m.aead.Seal(nonce, nonce, []byte(value), []byte(id))
}

// PutSecret 加密保存一个密钥；空值表示删除。
func (m *Memory) PutSecret(id, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == "" {
		delete(m.secrets, id)
		m.persist()
		return
	}
	m.secrets[id] = m.seal(id, value)
	m.persist()
}

// Secret 读取并解密一个密钥。
func (m *Memory) Secret(id string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sealed, ok := m.secrets[id]
	if !ok {
		return "", false
	}
	nonceSize := m.aead.NonceSize()
	if len(sealed) < nonceSize {
		return "", false
	}
	plaintext, err := m.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], []byte(id))
	if err != nil {
		return "", false
	}
	return string(plaintext), true
}

// TAPDSettings 返回保存的连接设置（不含密钥）。
func (m *Memory) TAPDSettings() domain.TAPDSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tapdSettings
}

// SetTAPDSettings 持久化连接设置。
func (m *Memory) SetTAPDSettings(settings domain.TAPDSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tapdSettings = settings
	m.persist()
}

// UpsertExternalItems 按外部 ID 去重合并一个项目的同步素材。
func (m *Memory) UpsertExternalItems(projectID string, items []domain.ExternalItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.externalItems[projectID]
	known := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		known[item.Kind+"/"+item.ID] = struct{}{}
	}
	for _, item := range items {
		item.ProjectID = projectID
		key := item.Kind + "/" + item.ID
		if _, ok := known[key]; ok {
			continue
		}
		known[key] = struct{}{}
		existing = append(existing, item)
	}
	m.externalItems[projectID] = existing
	m.persist()
}

// ExternalItems 返回一个项目已同步的素材。
func (m *Memory) ExternalItems(projectID string) []domain.ExternalItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.externalItems[projectID])
}

// AllExternalItems 返回全部项目的同步素材，按项目 ID 稳定排序。
func (m *Memory) AllExternalItems() []domain.ExternalItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]domain.ExternalItem, 0, 256)
	for _, items := range m.externalItems {
		all = append(all, items...)
	}
	slices.SortStableFunc(all, func(a, b domain.ExternalItem) int {
		if a.ProjectID != b.ProjectID {
			return strings.Compare(a.ProjectID, b.ProjectID)
		}
		return strings.Compare(a.Kind+b.ID, b.Kind+a.ID)
	})
	return all
}

// Project 按 ID 查找项目。
func (m *Memory) Project(id string) (domain.Project, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	index := slices.IndexFunc(m.projects, func(project domain.Project) bool {
		return project.ID == id
	})
	if index < 0 {
		return domain.Project{}, false
	}
	return m.projects[index], true
}

// Agents 返回全部智能体配置（不含密钥）。
func (m *Memory) Agents() []domain.AgentConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.agents)
}

// Agent 按 ID 查找智能体配置。
func (m *Memory) Agent(id string) (domain.AgentConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	index := slices.IndexFunc(m.agents, func(agent domain.AgentConfig) bool {
		return agent.ID == id
	})
	if index < 0 {
		return domain.AgentConfig{}, false
	}
	return m.agents[index], true
}

// PutAgent 新增或更新智能体配置；secret 非空时更新其 API Key。
// isDefault 为 true 时清除其他默认标记。返回保存后的配置。
func (m *Memory) PutAgent(agent domain.AgentConfig, secret string) domain.AgentConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Type = strings.TrimSpace(agent.Type)
	agent.BaseURL = strings.TrimRight(strings.TrimSpace(agent.BaseURL), "/")
	agent.Model = strings.TrimSpace(agent.Model)
	agent.Command = strings.TrimSpace(agent.Command)
	agent.ExtraArgs = strings.TrimSpace(agent.ExtraArgs)

	index := slices.IndexFunc(m.agents, func(existing domain.AgentConfig) bool {
		return existing.ID == agent.ID
	})
	if index < 0 {
		agent.ID = newID("agt")
	} else {
		agent.ID = m.agents[index].ID
		agent.APIKeySet = m.agents[index].APIKeySet || secret != ""
	}
	if secret != "" {
		keyID := SecretAgentPrefix + agent.ID
		m.secrets[keyID] = m.seal(keyID, secret)
		agent.APIKeySet = true
	}
	if agent.IsDefault {
		for i := range m.agents {
			m.agents[i].IsDefault = false
		}
	} else if len(m.agents) == 0 {
		agent.IsDefault = true
	}
	if index >= 0 {
		m.agents[index] = agent
	} else {
		m.agents = append(m.agents, agent)
	}
	m.persist()
	return agent
}

// DeleteAgent 删除智能体配置及其密钥。
func (m *Memory) DeleteAgent(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.agents, func(agent domain.AgentConfig) bool {
		return agent.ID == id
	})
	if index < 0 {
		return false
	}
	m.agents = slices.Delete(m.agents, index, index+1)
	delete(m.secrets, SecretAgentPrefix+id)
	m.persist()
	return true
}

// CreateRun 新增一次执行批次（最新在前）。
func (m *Memory) CreateRun(run domain.TestRun) domain.TestRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = newID("run")
	run.CreatedAt = time.Now()
	if m.runs == nil {
		m.runs = []domain.TestRun{}
	}
	m.runs = append([]domain.TestRun{run}, m.runs...)
	m.persist()
	return run
}

// SaveRun 整体更新一次执行批次。
func (m *Memory) SaveRun(run domain.TestRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index := range m.runs {
		if m.runs[index].ID == run.ID {
			m.runs[index] = run
			break
		}
	}
	m.persist()
}

// Run 按 ID 查找执行批次。
func (m *Memory) Run(id string) (domain.TestRun, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	index := slices.IndexFunc(m.runs, func(run domain.TestRun) bool {
		return run.ID == id
	})
	if index < 0 {
		return domain.TestRun{}, false
	}
	return m.runs[index], true
}

// UpdateProject 更新项目基础信息与上下文说明。
func (m *Memory) UpdateProject(id string, name, context string) (domain.Project, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.projects, func(project domain.Project) bool {
		return project.ID == id
	})
	if index < 0 {
		return domain.Project{}, false
	}
	if strings.TrimSpace(name) != "" {
		m.projects[index].Name = strings.TrimSpace(name)
	}
	m.projects[index].Context = context
	m.projects[index].UpdatedAt = time.Now()
	m.persist()
	return m.projects[index], true
}

// Environment 按 ID 查找测试环境。
func (m *Memory) Environment(id string) (domain.Environment, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	index := slices.IndexFunc(m.environments, func(environment domain.Environment) bool {
		return environment.ID == id
	})
	if index < 0 {
		return domain.Environment{}, false
	}
	return m.environments[index], true
}

// Runs 返回全部执行批次（最新在前）。
func (m *Memory) Runs() []domain.TestRun {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.runs)
}

// Knowledge 返回一个项目的全部知识条目（新在前）。
func (m *Memory) Knowledge(projectID string) []domain.KnowledgeEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries := make([]domain.KnowledgeEntry, 0)
	for _, entry := range m.knowledge {
		if entry.ProjectID == projectID {
			entries = append(entries, entry)
		}
	}
	return entries
}

// PutKnowledge 新增或更新知识条目，返回保存后的条目。
func (m *Memory) PutKnowledge(entry domain.KnowledgeEntry) domain.KnowledgeEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.Title = strings.TrimSpace(entry.Title)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Category = strings.TrimSpace(entry.Category)
	index := slices.IndexFunc(m.knowledge, func(existing domain.KnowledgeEntry) bool {
		return existing.ID == entry.ID
	})
	if index < 0 {
		entry.ID = newID("kn")
		entry.CreatedAt = time.Now()
		m.knowledge = append(m.knowledge, entry)
	} else {
		entry.ID = m.knowledge[index].ID
		entry.CreatedAt = m.knowledge[index].CreatedAt
		if entry.Source == "" {
			entry.Source = m.knowledge[index].Source
		}
		m.knowledge[index] = entry
	}
	m.persist()
	return entry
}

// DeleteKnowledge 删除一条知识条目。
func (m *Memory) DeleteKnowledge(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.knowledge, func(entry domain.KnowledgeEntry) bool {
		return entry.ID == id
	})
	if index < 0 {
		return false
	}
	m.knowledge = slices.Delete(m.knowledge, index, index+1)
	m.persist()
	return true
}

// EnvironmentKnowledge 返回一个环境的全部知识条目。
func (m *Memory) EnvironmentKnowledge(environmentID string) []domain.KnowledgeEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries := make([]domain.KnowledgeEntry, 0)
	for _, entry := range m.knowledge {
		if entry.EnvironmentID == environmentID {
			entries = append(entries, entry)
		}
	}
	return entries
}
