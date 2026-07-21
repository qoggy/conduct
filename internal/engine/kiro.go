package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

var ansiCSISequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// kiroEngine 通过 Kiro v3 的无头 chat 执行。v3 stdout 是本轮可见 assistant
// 文本的原始拼接；工具过程和诊断写入 stderr。
type kiroEngine struct{}

func (kiroEngine) Descriptor() EngineDescriptor {
	return EngineDescriptor{
		Name: "kiro",
		Capability: EngineCapability{
			AllowsModel: true,
			ModelSuggestions: []string{
				"auto",
				"claude-sonnet-5",
				"claude-opus-4.8",
				"gpt-5.6-sol",
				"gpt-5.6-terra",
				"gpt-5.6-luna",
			},
			AllowsEffort: true,
			EffortValues: []string{"low", "medium", "high", "xhigh", "max"},
		},
		IconFilename: "kiro.png",
	}
}

func (kiroEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	startedAt := time.Now()
	resultWithDuration := func() RunResult {
		return RunResult{DurationMilliseconds: time.Since(startedAt).Milliseconds()}
	}

	// Kiro v3 不支持 classic 的 --trust-all-tools。Conduct 持久合并官方 CI
	// 的 all/allow 规则，使默认 agent、用户 steering 和 session profile 都能复用。
	if err := ensureKiroFullPermissions(); err != nil {
		return resultWithDuration(), fmt.Errorf("failed to configure kiro v3 permissions: %w", err)
	}

	args := []string{
		"chat",
		"--v3",
		"--no-interactive",
		"--wrap", "never",
	}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort != "" {
		args = append(args, "--effort", request.Effort)
	}

	execution, infrastructureErr := runKiroCommand(ctx, commandSpec{
		binary: "kiro-cli",
		args:   args,
		stdin:  request.Prompt,
		dir:    request.WorkingDirectory,
	})
	result := resultWithDuration()
	var executionErrors []error
	if execution.commandErr != nil {
		executionErrors = append(executionErrors, kiroCommandError("kiro-cli", execution.output, execution.commandErr))
	}
	if execution.cleanupErr != nil {
		executionErrors = append(executionErrors, fmt.Errorf("failed to clean up kiro-cli process group: %w", execution.cleanupErr))
	}
	if infrastructureErr != nil {
		executionErrors = append(executionErrors, fmt.Errorf("failed to invoke kiro-cli: %w", infrastructureErr))
	}
	if len(executionErrors) != 0 {
		return result, errors.Join(executionErrors...)
	}

	result.Text = strings.TrimRight(execution.output.stdout, "\r\n")
	result.Tokens = nil
	result.SessionID = nil
	return result, nil
}

type kiroCommandExecution struct {
	output     commandOutput
	commandErr error
	cleanupErr error
}

// runKiroCommand 使用已 unlink 的普通临时文件连接三条标准流。Kiro v3 内部的
// ACP server 可能继承 stdout/stderr；普通文件可避免 os/exec 等待匿名 pipe EOF。
// 每次调用还拥有独立进程组，顶层进程结束后清理仍存活的 ACP 子进程。
func runKiroCommand(ctx context.Context, spec commandSpec) (execution kiroCommandExecution, infrastructureErr error) {
	stdinFile, err := createUnlinkedTempFile("conduct-kiro-stdin-")
	if err != nil {
		return execution, err
	}
	stdoutFile, err := createUnlinkedTempFile("conduct-kiro-stdout-")
	if err != nil {
		if closeErr := stdinFile.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close Kiro stdin temporary file: %w", closeErr))
		}
		return execution, err
	}
	stderrFile, err := createUnlinkedTempFile("conduct-kiro-stderr-")
	if err != nil {
		closeErr := errors.Join(stdinFile.Close(), stdoutFile.Close())
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close Kiro temporary files: %w", closeErr))
		}
		return execution, err
	}
	defer func() {
		closeErr := errors.Join(stdinFile.Close(), stdoutFile.Close(), stderrFile.Close())
		if closeErr != nil {
			infrastructureErr = errors.Join(infrastructureErr, fmt.Errorf("failed to close Kiro temporary files: %w", closeErr))
		}
	}()

	if _, err := io.WriteString(stdinFile, spec.stdin); err != nil {
		return execution, fmt.Errorf("failed to write Kiro stdin temporary file: %w", err)
	}
	if _, err := stdinFile.Seek(0, io.SeekStart); err != nil {
		return execution, fmt.Errorf("failed to rewind Kiro stdin temporary file: %w", err)
	}

	cmd := exec.CommandContext(ctx, spec.binary, spec.args...)
	if spec.dir != "" {
		cmd.Dir = spec.dir
	}
	cmd.Stdin = stdinFile
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	startedAt := time.Now()
	execution.commandErr = cmd.Run()
	execution.output.durationMs = time.Since(startedAt).Milliseconds()
	if cmd.Process != nil {
		execution.cleanupErr = terminateKiroProcessGroup(cmd.Process.Pid)
	}

	stdout, err := readTemporaryOutput(stdoutFile, "stdout")
	if err != nil {
		return execution, err
	}
	stderr, err := readTemporaryOutput(stderrFile, "stderr")
	if err != nil {
		return execution, err
	}
	execution.output.stdout = stdout
	execution.output.stderr = stderr
	return execution, nil
}

func createUnlinkedTempFile(pattern string) (*os.File, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kiro temporary file: %w", err)
	}
	if err := os.Remove(file.Name()); err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("failed to unlink Kiro temporary file: %w", err),
				fmt.Errorf("failed to close Kiro temporary file: %w", closeErr),
			)
		}
		return nil, fmt.Errorf("failed to unlink Kiro temporary file: %w", err)
	}
	return file, nil
}

func readTemporaryOutput(file *os.File, streamName string) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to rewind Kiro %s temporary file: %w", streamName, err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read Kiro %s temporary file: %w", streamName, err)
	}
	return string(data), nil
}

func terminateKiroProcessGroup(processID int) error {
	err := syscall.Kill(-processID, syscall.SIGTERM)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func ensureKiroFullPermissions() error {
	kiroHome, err := kiroHomeDirectory()
	if err != nil {
		return err
	}
	permissionsPath := filepath.Join(kiroHome, "settings", "permissions.yaml")

	data, err := os.ReadFile(permissionsPath)
	fileMode := os.FileMode(0o600)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", permissionsPath, err)
	}
	if err == nil {
		info, statErr := os.Stat(permissionsPath)
		if statErr != nil {
			return fmt.Errorf("failed to stat %s: %w", permissionsPath, statErr)
		}
		fileMode = info.Mode().Perm()
	}

	document, rules, err := parseKiroPermissions(data, permissionsPath)
	if err != nil {
		return err
	}
	if hasKiroFullPermission(rules) {
		return nil
	}
	rules.Content = append(rules.Content, kiroFullPermissionNode())
	updated, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", permissionsPath, err)
	}
	if err := atomicWriteKiroPermissions(permissionsPath, updated, fileMode); err != nil {
		return err
	}
	return nil
}

func kiroHomeDirectory() (string, error) {
	if kiroHome := os.Getenv("KIRO_HOME"); kiroHome != "" {
		return kiroHome, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".kiro"), nil
}

func parseKiroPermissions(data []byte, path string) (*yaml.Node, *yaml.Node, error) {
	document := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	document.Content = []*yaml.Node{root}
	if len(strings.TrimSpace(string(data))) != 0 {
		if err := yaml.Unmarshal(data, document); err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
			return nil, nil, fmt.Errorf("failed to parse %s: root must be a YAML mapping", path)
		}
		root = document.Content[0]
	}

	for index := 0; index < len(root.Content); index += 2 {
		if root.Content[index].Value != "rules" {
			continue
		}
		rules := root.Content[index+1]
		if rules.Kind == yaml.ScalarNode && rules.Tag == "!!null" {
			rules.Kind = yaml.SequenceNode
			rules.Tag = "!!seq"
			rules.Value = ""
		}
		if rules.Kind != yaml.SequenceNode {
			return nil, nil, fmt.Errorf("failed to parse %s: rules must be a YAML sequence", path)
		}
		return document, rules, nil
	}

	rulesKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rules"}
	rules := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, rulesKey, rules)
	return document, rules, nil
}

func hasKiroFullPermission(rules *yaml.Node) bool {
	for _, rule := range rules.Content {
		if rule.Kind != yaml.MappingNode {
			continue
		}
		var capability, effect string
		for index := 0; index < len(rule.Content); index += 2 {
			switch rule.Content[index].Value {
			case "capability":
				capability = rule.Content[index+1].Value
			case "effect":
				effect = rule.Content[index+1].Value
			}
		}
		if capability == "all" && effect == "allow" {
			return true
		}
	}
	return false
}

func kiroFullPermissionNode() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "capability"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "all"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "effect"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "allow"},
		},
	}
}

func atomicWriteKiroPermissions(path string, data []byte, mode os.FileMode) (returnErr error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", directory, err)
	}
	temporaryFile, err := os.CreateTemp(directory, ".permissions-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary permissions file in %s: %w", directory, err)
	}
	temporaryPath := temporaryFile.Name()
	defer func() {
		if temporaryFile != nil {
			if closeErr := temporaryFile.Close(); closeErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("failed to close %s: %w", temporaryPath, closeErr))
			}
		}
		if temporaryPath != "" {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, fmt.Errorf("failed to remove %s: %w", temporaryPath, removeErr))
			}
		}
	}()

	if err := temporaryFile.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", temporaryPath, err)
	}
	if _, err := temporaryFile.Write(data); err != nil {
		return fmt.Errorf("failed to write %s: %w", temporaryPath, err)
	}
	if err := temporaryFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync %s: %w", temporaryPath, err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", temporaryPath, err)
	}
	temporaryFile = nil
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("failed to commit %s: %w", path, err)
	}
	temporaryPath = ""
	return nil
}

func kiroCommandError(commandName string, output commandOutput, err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		diagnostic := strings.TrimSpace(cleanTerminalOutput(output.stderr))
		if diagnostic == "" {
			return fmt.Errorf("%s exited with code %d", commandName, exitError.ExitCode())
		}
		return fmt.Errorf("%s exited with code %d: %s", commandName, exitError.ExitCode(), truncate(diagnostic, 500))
	}
	return fmt.Errorf("failed to invoke kiro-cli: %w", err)
}

func cleanTerminalOutput(value string) string {
	return ansiCSISequence.ReplaceAllString(value, "")
}

func init() { Register(kiroEngine{}) }
