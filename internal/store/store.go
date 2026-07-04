// Package store 是 ~/.conduct 下的托管工作流 store：纯文件、按名寻址，无数据库。
//
// 只负责持久化与系统元数据（createdAt / updatedAt）的写入；定义的语义校验由 workflow.Validate
// 在命令层把关（store 不重复校验，见各 CLI 命令）。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qoggy/conduct/internal/workflow"
)

var (
	// ErrExists 表示目标工作流名已被占用。
	ErrExists = errors.New("工作流已存在")
	// ErrNotExist 表示目标工作流不存在。
	ErrNotExist = errors.New("工作流不存在")
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
		return nil, fmt.Errorf("解析用户主目录失败: %w", err)
	}
	return New(filepath.Join(home, ".conduct")), nil
}

// New 用给定根目录构造 store。
func New(root string) *Store {
	return &Store{root: root, now: time.Now}
}

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

// Create 新建一份工作流：要求 def.Name 已设且未占用；写入 createdAt / updatedAt 与规范化形态。
func (s *Store) Create(def *workflow.Definition) error {
	if err := workflow.ValidateName(def.Name); err != nil {
		return err
	}
	if s.Exists(def.Name) {
		return fmt.Errorf("%s: %w", def.Name, ErrExists)
	}
	stamp := s.now().Format(time.RFC3339)
	def.CreatedAt = stamp
	def.UpdatedAt = stamp
	def.Normalize()
	return s.write(def)
}

// Save 覆盖既有工作流（edit）：保留原 createdAt、重戳 updatedAt。
func (s *Store) Save(def *workflow.Definition) error {
	if err := workflow.ValidateName(def.Name); err != nil {
		return err
	}
	existing, err := s.Load(def.Name)
	if err != nil {
		return err
	}
	def.CreatedAt = existing.CreatedAt
	def.UpdatedAt = s.now().Format(time.RFC3339)
	def.Normalize()
	return s.write(def)
}

// Load 读入一份工作流；不存在时返回 ErrNotExist。
func (s *Store) Load(name string) (*workflow.Definition, error) {
	if err := workflow.ValidateName(name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", name, ErrNotExist)
		}
		return nil, fmt.Errorf("读取工作流 %s 失败: %w", name, err)
	}
	def, err := workflow.ParseDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("工作流 %s 内容损坏: %w", name, err)
	}
	return def, nil
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
		return fmt.Errorf("%s: %w", oldName, ErrNotExist)
	}
	if s.Exists(newName) {
		return fmt.Errorf("%s: %w", newName, ErrExists)
	}
	def, err := s.Load(oldName)
	if err != nil {
		return err
	}
	def.Name = newName
	def.UpdatedAt = s.now().Format(time.RFC3339)
	// 先写新文件，成功后再删旧文件——中途失败时旧文件仍完好。
	if err := s.write(def); err != nil {
		return err
	}
	if err := os.Remove(s.path(oldName)); err != nil {
		return fmt.Errorf("删除旧文件 %s 失败: %w", oldName, err)
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
			return fmt.Errorf("%s: %w", name, ErrNotExist)
		}
		return fmt.Errorf("删除工作流 %s 失败: %w", name, err)
	}
	return nil
}

// List 列出 store 内全部工作流，按名字排序；store 为空 / 目录尚未创建时返回空切片、不报错。
// 单个文件解析失败不连累其余：跳过并计入第二个返回值 skipped（每项一个解析错误），由调用方
// 决定如何告警。第三个返回值仅在目录不可读等致命情形非 nil。
func (s *Store) List() ([]*workflow.Definition, []error, error) {
	entries, err := os.ReadDir(s.workflowsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("读取 store 失败: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
	}
	sort.Strings(names)
	defs := make([]*workflow.Definition, 0, len(names))
	var skipped []error
	for _, name := range names {
		def, err := s.Load(name)
		if err != nil {
			skipped = append(skipped, err) // 单个文件损坏不连累其余
			continue
		}
		defs = append(defs, def)
	}
	return defs, skipped, nil
}

// write 把定义规范化落盘（原子写：临时文件 + rename），首用自动建目录。
func (s *Store) write(def *workflow.Definition) error {
	if err := os.MkdirAll(s.workflowsDir(), 0o755); err != nil {
		return fmt.Errorf("创建 store 目录失败: %w", err)
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化工作流 %s 失败: %w", def.Name, err)
	}
	data = append(data, '\n')
	return atomicWrite(s.path(def.Name), data) // 原子写实现见 runs.go
}
