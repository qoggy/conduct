package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/qoggy/conduct/internal/run"
)

// 运行记录持久化：与 workflows 同根（~/.conduct/runs/<id>/），一次运行一个目录，含
// run.json（概要）、trace.jsonl（逐步）、run-summary.md（报告）。见 spec〈落盘存储结构〉。

var (
	// ErrRunExists 表示目标 run id 已被占用（不覆盖历史）。
	ErrRunExists = errors.New("运行已存在")
	// ErrRunNotExist 表示目标运行记录不存在。
	ErrRunNotExist = errors.New("运行不存在")
)

func (s *Store) runsDir() string { return filepath.Join(s.root, "runs") }

// runDir 返回某 run 的目录路径；id 非法（防路径穿越）时报错。
func (s *Store) runDir(id string) (string, error) {
	if err := run.ValidateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.runsDir(), id), nil
}

func runJSONPath(dir string) string { return filepath.Join(dir, "run.json") }
func tracePath(dir string) string   { return filepath.Join(dir, "trace.jsonl") }
func summaryPath(dir string) string { return filepath.Join(dir, "run-summary.md") }

// SummaryPath 返回某 run 的 run-summary.md 绝对路径（供 CLI 收尾提示）；id 非法时报错。
func (s *Store) SummaryPath(id string) (string, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return "", err
	}
	return summaryPath(dir), nil
}

// CreateRun 新建一次运行的目录并写入初始 run.json（开跑即写，status=running）+ 空 trace.jsonl。
// 目录已存在即报错（run id 撞车不静默覆盖历史）。
func (s *Store) CreateRun(record *run.Record) error {
	dir, err := s.runDir(record.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.runsDir(), 0o755); err != nil {
		return fmt.Errorf("创建 runs 目录失败: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s: %w", record.ID, ErrRunExists)
		}
		return fmt.Errorf("创建运行目录 %s 失败: %w", record.ID, err)
	}
	if err := os.WriteFile(tracePath(dir), nil, 0o644); err != nil {
		return fmt.Errorf("初始化 trace.jsonl 失败: %w", err)
	}
	return s.WriteRun(record)
}

// WriteRun 原子重写 run.json（增量更新 artifacts / 收尾写终态都走它）。
func (s *Store) WriteRun(record *run.Record) error {
	dir, err := s.runDir(record.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化运行 %s 失败: %w", record.ID, err)
	}
	return atomicWrite(runJSONPath(dir), append(data, '\n'))
}

// AppendTrace 向 trace.jsonl 追加一条步骤记录（单行 JSON）。
func (s *Store) AppendTrace(id string, entry run.TraceEntry) error {
	dir, err := s.runDir(id)
	if err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("序列化 trace 条目失败: %w", err)
	}
	file, err := os.OpenFile(tracePath(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 trace.jsonl 失败: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("追加 trace 失败: %w", err)
	}
	return nil
}

// WriteSummary 写 run-summary.md（收尾生成）。
func (s *Store) WriteSummary(id, markdown string) error {
	dir, err := s.runDir(id)
	if err != nil {
		return err
	}
	return atomicWrite(summaryPath(dir), []byte(markdown))
}

// LoadRun 读入某 run 的 run.json；不存在时返回 ErrNotExist。
func (s *Store) LoadRun(id string) (*run.Record, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(runJSONPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", id, ErrRunNotExist)
		}
		return nil, fmt.Errorf("读取运行 %s 失败: %w", id, err)
	}
	var record run.Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("运行 %s 的 run.json 损坏: %w", id, err)
	}
	return &record, nil
}

// LoadTrace 读入某 run 的 trace.jsonl（逐行解析）；文件缺失视为空 trace（尚未写入任何步骤）。
func (s *Store) LoadTrace(id string) ([]run.TraceEntry, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(tracePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 trace %s 失败: %w", id, err)
	}
	defer file.Close()
	var entries []run.TraceEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // 放宽单行上限：产物可能很长
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry run.TraceEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("trace %s 第 %d 行损坏: %w", id, lineNumber, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("扫描 trace %s 失败: %w", id, err)
	}
	return entries, nil
}

// ListRuns 列出全部运行记录，按 startedAt 倒序（新在前）；目录不存在返回空。
// 单个 run.json 损坏不连累其余：跳过并计入 skipped。
func (s *Store) ListRuns() ([]*run.Record, []error, error) {
	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("读取 runs 目录失败: %w", err)
	}
	var records []*run.Record
	var skipped []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, err := s.LoadRun(entry.Name())
		if err != nil {
			skipped = append(skipped, err)
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return startedAfter(records[i].StartedAt, records[j].StartedAt) // 新在前
	})
	return records, skipped, nil
}

// startedAfter 报告 a 是否晚于 b（按 RFC3339 解析比较真实时刻；解析失败退化为字符串比较，
// 不同时区偏移下字典序会失真，故不裸用字典序）。
func startedAfter(a, b string) bool {
	timeA, errA := time.Parse(time.RFC3339, a)
	timeB, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a > b
	}
	return timeA.After(timeB)
}

// atomicWrite 原子写文件（临时文件 + rename），与 workflow write 同一策略。
func atomicWrite(finalPath string, data []byte) error {
	tempPath := finalPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", finalPath, err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			return fmt.Errorf("提交 %s 失败: %w（且清理临时文件失败: %v）", finalPath, err, removeErr)
		}
		return fmt.Errorf("提交 %s 失败: %w", finalPath, err)
	}
	return nil
}
