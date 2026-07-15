package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/run"
)

// 运行记录持久化：与 workflows 同根（~/.conduct/runs/<id>/），一次运行一个目录，含
// run.json（概要）、trace.jsonl（逐节点尝试）、run-summary.md（报告）。见 spec〈落盘存储结构〉。

var (
	// ErrRunExists 表示目标 run id 已被占用（不覆盖历史）。
	ErrRunExists = apperror.New(apperror.CodeRunAlreadyExists, nil)
	// ErrRunNotExist 表示目标运行记录不存在。
	ErrRunNotExist = apperror.New(apperror.CodeRunNotFound, nil)
	// ErrSummaryNotExist 表示 run-summary.md 尚未生成（多为 running 期，收尾节点还没写）。
	// 供 handler 映射 404，与「运行本身不存在」区分。
	ErrSummaryNotExist = apperror.New(apperror.CodeRunSummaryNotFound, nil)
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
	if err := record.ValidateLanguage(); err != nil {
		return err
	}
	dir, err := s.runDir(record.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.runsDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create runs directory: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return apperror.New(apperror.CodeRunAlreadyExists, apperror.Params{"id": record.ID})
		}
		return fmt.Errorf("failed to create run directory %s: %w", record.ID, err)
	}
	if err := os.WriteFile(tracePath(dir), nil, 0o644); err != nil {
		return fmt.Errorf("failed to initialize trace.jsonl: %w", err)
	}
	return s.WriteRun(record)
}

// RemoveRun 删除一条运行记录的整个目录（runs/<id>/，三件套连同目录一并移除）；不存在时返回
// ErrRunNotExist。id 非法（防路径穿越）时报错。其它运行分毫不动——各 run 靠自身快照自解释、互不依赖。
func (s *Store) RemoveRun(id string) error {
	dir, err := s.runDir(id)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return apperror.New(apperror.CodeRunNotFound, apperror.Params{"id": id})
		}
		return fmt.Errorf("failed to access run directory %s: %w", id, statErr)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to remove run directory %s: %w", id, err)
	}
	return nil
}

// WriteRun 原子重写 run.json（增量更新 artifacts / 收尾写终态都走它）。
func (s *Store) WriteRun(record *run.Record) error {
	if err := record.ValidateLanguage(); err != nil {
		return err
	}
	dir, err := s.runDir(record.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode run %s: %w", record.ID, err)
	}
	return atomicWrite(runJSONPath(dir), append(data, '\n'))
}

// AppendTrace 向 trace.jsonl 追加一条步骤记录（单行 JSON）。
func (s *Store) AppendTrace(id string, entry run.TraceEntry) (err error) {
	dir, err := s.runDir(id)
	if err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to encode trace entry: %w", err)
	}
	file, err := os.OpenFile(tracePath(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open trace.jsonl: %w", err)
	}
	// 写路径上 Close 失败意味着这行 trace 可能没落全，不能像读路径那样静默丢弃：合并进返回值。
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close trace.jsonl: %w", cerr)
		}
	}()
	if _, werr := file.Write(append(line, '\n')); werr != nil {
		return fmt.Errorf("failed to append trace entry: %w", werr)
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

// RemoveSummary 删除某 run 的 run-summary.md；文件不存在视为已删除，供 resume 切回 running 时清理旧终态报告。
func (s *Store) RemoveSummary(id string) error {
	dir, err := s.runDir(id)
	if err != nil {
		return err
	}
	if err := os.Remove(summaryPath(dir)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to remove run summary %s: %w", id, err)
	}
	return nil
}

// ReadSummary 读 run-summary.md 全文；尚未生成（running 期收尾节点还没写）返回 ErrSummaryNotExist。
// WriteSummary 走 atomicWrite（rename 原子），故不存在读到半文件的情况。
func (s *Store) ReadSummary(id string) (string, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(summaryPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", apperror.New(apperror.CodeRunSummaryNotFound, apperror.Params{"id": id})
		}
		return "", fmt.Errorf("failed to read run summary %s: %w", id, err)
	}
	return string(data), nil
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
			return nil, apperror.New(apperror.CodeRunNotFound, apperror.Params{"id": id})
		}
		return nil, fmt.Errorf("failed to read run %s: %w", id, err)
	}
	var record run.Record
	if err := decodeStrictJSON(data, &record); err != nil {
		return nil, fmt.Errorf("run.json for run %s is corrupted: %w", id, err)
	}
	if err := record.ValidateLanguage(); err != nil {
		return nil, fmt.Errorf("run.json for run %s is corrupted: %w", id, err)
	}
	return &record, nil
}

// LoadTrace 读入某 run 的 trace.jsonl（逐行解析）；文件缺失视为空 trace（尚未写入任何步骤）。
// 用 bufio.Reader.ReadBytes('\n') 而非 Scanner：单行产物可达 MB 级，Scanner 的 token 上限会 ErrTooLong 崩；
// 且只解析以换行结尾的完整行——末尾无换行的残块视为「正在写入的半行」丢弃，不当成损坏报错。
func (s *Store) LoadTrace(id string) ([]run.TraceEntry, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(tracePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []run.TraceEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read trace %s: %w", id, err)
	}
	defer file.Close()
	entries := make([]run.TraceEntry, 0)
	reader := bufio.NewReader(file)
	for lineNumber := 1; ; lineNumber++ {
		chunk, readErr := reader.ReadBytes('\n')
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			line := bytes.TrimRight(chunk[:len(chunk)-1], "\r") // 容忍 \r\n
			if len(line) > 0 {
				var entry run.TraceEntry
				if err := json.Unmarshal(line, &entry); err != nil {
					return nil, fmt.Errorf("trace %s is corrupted at line %d: %w", id, lineNumber, err)
				}
				entries = append(entries, entry)
			}
		}
		// chunk 不以 '\n' 结尾即末尾半行（尚未写完）：不解析、直接随 EOF 收束。
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return entries, nil
			}
			return nil, fmt.Errorf("failed to read trace %s: %w", id, readErr)
		}
	}
}

// CountTrace 统计某 run 的 trace 完整行数（= 落盘的物理记录条数，含 resume 后同一 nodeId 的多条）。
// 只数 '\n' 字节、绝不解析 JSON——单行可达 MB 级，全量 LoadTrace 只为数行数会拖垮列表。
// 文件缺失视为 0；末尾无换行的半行（正在写入）自然不计入。
// 注意：进度分子 k 已改用按 nodeId 去重的 CountProgress（防 resume 后 k>N，见其文档与 cli-runtime.md
// 〈run resume〉），本函数数的是全量物理行数，语义是「已落盘多少条 trace」，非「完成到第几步」。
func (s *Store) CountTrace(id string) (int, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return 0, err
	}
	file, err := os.Open(tracePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read trace %s: %w", id, err)
	}
	defer file.Close()
	count := 0
	buf := make([]byte, 64*1024)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			count += bytes.Count(buf[:n], []byte{'\n'})
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return count, nil
			}
			return 0, fmt.Errorf("failed to count lines in trace %s: %w", id, readErr)
		}
	}
}

// progressLine 只取算进度所需的两字段：逐行流式解析 trace.jsonl 时避免把 MB 级 input/output 也解出来。
type progressLine struct {
	NodeID  string `json:"nodeId"`
	Success bool   `json:"success"`
}

// CountProgress 统计一次运行的进度分子 k = trace 中「唯一 nodeId 且（最后一次记录）success」的节点数
// （去重逻辑同 run.ProgressCount）。列表页为每个 run 算进度时用它替代 CountTrace 的数行数——resume 保留
// 失败行 + 续写补跑行会让同一 nodeId 出现多条，数行数会使 k 越过分母 N；按 nodeId 去重保 k ≤ N。
// 逐行流式解析、只取 nodeId/success 两字段（不 materialize MB 级 input/output）；行读取健壮性同
// LoadTrace（只认完整行、容忍 \r\n、末尾半行丢弃不报损坏）。文件缺失视为 0。
func (s *Store) CountProgress(id string) (int, error) {
	dir, err := s.runDir(id)
	if err != nil {
		return 0, err
	}
	file, err := os.Open(tracePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read trace %s: %w", id, err)
	}
	defer file.Close()
	lastSuccess := map[string]bool{}
	reader := bufio.NewReader(file)
	for lineNumber := 1; ; lineNumber++ {
		chunk, readErr := reader.ReadBytes('\n')
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			line := bytes.TrimRight(chunk[:len(chunk)-1], "\r")
			if len(line) > 0 {
				var pl progressLine
				if err := json.Unmarshal(line, &pl); err != nil {
					return 0, fmt.Errorf("trace %s is corrupted at line %d: %w", id, lineNumber, err)
				}
				lastSuccess[pl.NodeID] = pl.Success // 后写覆盖前写，末条为准
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				count := 0
				for _, ok := range lastSuccess {
					if ok {
						count++
					}
				}
				return count, nil
			}
			return 0, fmt.Errorf("failed to read trace %s: %w", id, readErr)
		}
	}
}

// ListRuns 列出全部运行记录，按 startedAt 倒序（新在前）；目录不存在返回空。
// 单个 run.json 损坏不连累其余：跳过并计入 skipped。
func (s *Store) ListRuns() ([]*run.Record, []error, error) {
	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read runs directory: %w", err)
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
		return fmt.Errorf("failed to write %s: %w", finalPath, err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			return fmt.Errorf("failed to commit %s: %w (and failed to remove temporary file: %v)", finalPath, err, removeErr)
		}
		return fmt.Errorf("failed to commit %s: %w", finalPath, err)
	}
	return nil
}
