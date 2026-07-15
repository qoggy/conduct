// Package store 是 ~/.conduct 下的托管工作流 store：纯文件、按名寻址，无数据库。
//
// 只负责持久化与系统元数据（createdAt / updatedAt）的写入；定义的语义校验由 workflow.Validate
// 在命令层把关（store 不重复校验，见各 CLI 命令）。
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
)

var (
	// ErrExists 表示目标工作流名已被占用。
	ErrExists = apperror.New(apperror.CodeWorkflowAlreadyExists, nil)
	// ErrNotExist 表示目标工作流不存在。
	ErrNotExist = apperror.New(apperror.CodeWorkflowNotFound, nil)
)

// Store 指向一个工作流 store 根目录（生产为 ~/.conduct，测试可指向临时目录）。
type Store struct {
	root string
	now  func() time.Time
}

// Default 返回生产 store，根目录固定为 ~/.conduct。
func Default() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return New(filepath.Join(home, ".conduct")), nil
}

// New 用给定根目录构造 store。
func New(root string) *Store {
	return &Store{root: root, now: time.Now}
}

// Root 返回 store 根目录，供同属 ~/.conduct 的共享设置基础设施复用。
func (s *Store) Root() string { return s.root }

func (s *Store) workflowsDir() string { return filepath.Join(s.root, "workflows") }

func (s *Store) path(name string) string {
	return filepath.Join(s.workflowsDir(), name+".json")
}

// Exists 报告某名字的工作流是否已入库。非法名一律视为不存在（不触碰文件系统）。
func (s *Store) Exists(name string) bool {
	if err := workflow.ValidateName(name); err != nil {
		return false
	}
	_, err := os.Stat(s.path(name))
	return err == nil
}

// Create 新建一份工作流：要求 wf.Name 已设且未占用；写入 createdAt / updatedAt 与规范化形态。
func (s *Store) Create(wf *workflow.Workflow) error {
	if err := workflow.ValidateName(wf.Name); err != nil {
		return err
	}
	if s.Exists(wf.Name) {
		return apperror.New(apperror.CodeWorkflowAlreadyExists, apperror.Params{"name": wf.Name})
	}
	stamp := s.now().Format(time.RFC3339)
	wf.CreatedAt = stamp
	wf.UpdatedAt = stamp
	return s.write(wf)
}

// Save 覆盖一份能被严格解码的既有工作流：保留原 createdAt、重戳 updatedAt。
// 粒度编辑命令先 Load 再改，走此方法；要用完整定义修复结构损坏的文件，走 ReplaceDefinition。
func (s *Store) Save(wf *workflow.Workflow) error {
	if err := workflow.ValidateName(wf.Name); err != nil {
		return err
	}
	existing, err := s.Load(wf.Name)
	if err != nil {
		return err
	}
	wf.CreatedAt = existing.CreatedAt
	wf.UpdatedAt = s.now().Format(time.RFC3339)
	return s.write(wf)
}

// ReplaceDefinition 用一份完整定义覆盖既有工作流，是 edit / UI 整体保存的恢复通道。它只要求目标文件
// 存在，不要求旧内容能被严格解码：旧 JSON 仍能读出 createdAt 时予以保留；结构已坏到无法读取元数据时，
// 以本次替换时刻重新建立 createdAt。updatedAt 始终重戳。新定义的语义校验由调用方在进入本方法前完成。
func (s *Store) ReplaceDefinition(wf *workflow.Workflow) error {
	if err := workflow.ValidateName(wf.Name); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path(wf.Name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": wf.Name})
		}
		return fmt.Errorf("failed to read workflow %s for replacement: %w", wf.Name, err)
	}

	stamp := s.now().Format(time.RFC3339)
	var metadata struct {
		CreatedAt string `json:"createdAt"`
	}
	metadataErr := json.Unmarshal(data, &metadata)
	if metadataErr == nil && metadata.CreatedAt != "" {
		wf.CreatedAt = metadata.CreatedAt
	} else {
		// 这是 edit 的显式恢复语义：旧文件损坏不能阻止合法新定义落盘；无法可信读取的旧元数据不沿用。
		wf.CreatedAt = stamp
	}
	wf.UpdatedAt = stamp
	return s.write(wf)
}

// decodeStrictJSON 把 data 严格解码为 *T：拒绝未知字段与多余尾随内容。落盘文件（工作流 / run.json）一律走
// 它，令旧格式 / 拼写错误 / 结构损坏 fail-loud，不静默解成半空对象（conduct 不兼容旧格式，宁可报错也不带病读）。
func decodeStrictJSON[T any](data []byte, dst *T) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing content (expected a single JSON object)")
	}
	return nil
}

// Load 读入一份工作流（完整记录）；不存在时返回 ErrNotExist。文件内容为完整记录
// {name, createdAt, updatedAt, definition}；ParseDefinition 只取定义主体，故此处直接解 Workflow。
func (s *Store) Load(name string) (*workflow.Workflow, error) {
	if err := workflow.ValidateName(name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": name})
		}
		return nil, fmt.Errorf("failed to read workflow %s: %w", name, err)
	}
	var wf workflow.Workflow
	if err := decodeStrictJSON(data, &wf); err != nil {
		return nil, fmt.Errorf("workflow %s is corrupted: %w", name, err)
	}
	return &wf, nil
}

// Rename 改名：old 须存在、new 须未占用；保留 createdAt、重戳 updatedAt、改内部 name。
func (s *Store) Rename(oldName, newName string) error {
	if err := workflow.ValidateName(oldName); err != nil {
		return err
	}
	if err := workflow.ValidateName(newName); err != nil {
		return err
	}
	if !s.Exists(oldName) {
		return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": oldName})
	}
	if s.Exists(newName) {
		return apperror.New(apperror.CodeWorkflowAlreadyExists, apperror.Params{"name": newName})
	}
	wf, err := s.Load(oldName)
	if err != nil {
		return err
	}
	wf.Name = newName
	wf.UpdatedAt = s.now().Format(time.RFC3339)
	// 先写新文件，成功后再删旧文件——中途失败时旧文件仍完好。
	if err := s.write(wf); err != nil {
		return err
	}
	if err := os.Remove(s.path(oldName)); err != nil {
		return fmt.Errorf("failed to remove old workflow file %s: %w", oldName, err)
	}
	return nil
}

// Delete 删除一份工作流；不存在时返回 ErrNotExist。
func (s *Store) Delete(name string) error {
	if err := workflow.ValidateName(name); err != nil {
		return err
	}
	if err := os.Remove(s.path(name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": name})
		}
		return fmt.Errorf("failed to delete workflow %s: %w", name, err)
	}
	return nil
}

// List 列出 store 内全部工作流，按 updatedAt 倒序（最近修改在前，updatedAt 相同再按 name 升序兜底）；
// store 为空 / 目录尚未创建时返回空切片、不报错。单个文件解析失败不连累其余：跳过并计入第二个返回值
// skipped（每项一个解析错误），由调用方决定如何告警。第三个返回值仅在目录不可读等致命情形非 nil。
//
// 排序须先加载各 workflow 取 updatedAt 再排（而非加载前按名排）——与 ListRuns 的时间倒序同一「最近优先」
// 心智，但比较字段是 updatedAt（不复用比较 startedAt 的 startedAfter）。一处改则 CLI workflow list 与 UI
// 工作流列表（handleListWorkflows 直接沿用本顺序、前端不二次排序）同源同序。
func (s *Store) List() ([]*workflow.Workflow, []error, error) {
	entries, err := os.ReadDir(s.workflowsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read workflow store: %w", err)
	}
	workflows := make([]*workflow.Workflow, 0, len(entries))
	var skipped []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		wf, err := s.Load(name)
		if err != nil {
			skipped = append(skipped, err) // 单个文件损坏不连累其余
			continue
		}
		workflows = append(workflows, wf)
	}
	sort.SliceStable(workflows, func(i, j int) bool {
		if workflows[i].UpdatedAt != workflows[j].UpdatedAt {
			return updatedAfter(workflows[i].UpdatedAt, workflows[j].UpdatedAt) // 最近修改在前
		}
		return workflows[i].Name < workflows[j].Name // updatedAt 相同按 name 升序兜底，免同刻并列抖动
	})
	return workflows, skipped, nil
}

// updatedAfter 报告 a 是否晚于 b（按 RFC3339 解析比较真实时刻；解析失败退化为字符串比较，不同时区
// 偏移下字典序会失真，故不裸用字典序）。与 runs.go 的 startedAfter 同策略，但语义是 updatedAt 比较。
func updatedAfter(a, b string) bool {
	timeA, errA := time.Parse(time.RFC3339, a)
	timeB, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a > b
	}
	return timeA.After(timeB)
}

// write 把完整记录落盘（原子写：临时文件 + rename），首用自动建目录。
func (s *Store) write(wf *workflow.Workflow) error {
	if err := os.MkdirAll(s.workflowsDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create workflow store directory: %w", err)
	}
	data, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode workflow %s: %w", wf.Name, err)
	}
	data = append(data, '\n')
	return atomicWrite(s.path(wf.Name), data) // 原子写实现见 runs.go
}
