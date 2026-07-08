package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeDetachLauncher 按预设返回三态 (runID, note, err)，替掉真 self-exec；同时记录入参供断言。
type fakeDetachLauncher struct {
	runID, note string
	err         error

	gotName, gotPrompt, gotCwd string
	gotResumeID                string
}

func (f *fakeDetachLauncher) Launch(name, userPrompt, absCwd string) (string, string, error) {
	f.gotName, f.gotPrompt, f.gotCwd = name, userPrompt, absCwd
	return f.runID, f.note, f.err
}

func (f *fakeDetachLauncher) LaunchResume(id string) (string, string, error) {
	f.gotResumeID = id
	return f.runID, f.note, f.err
}

func newDetachTestCmd() (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	return cmd, &out
}

// 发射失败（err 非空）→ 退 1：直接透传发射器错误、不再包装，且不往 stdout 写句柄。
func TestRunDetachedWithLaunchError(t *testing.T) {
	cmd, out := newDetachTestCmd()
	sentinel := errors.New("运行启动失败：no such file")
	err := runDetachedWith(cmd, &fakeDetachLauncher{err: sentinel}, "flow", "需求", "/proj", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("应透传发射器错误，得到 %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("发射失败不应往 stdout 写句柄，得到 %q", out.String())
	}
}

// 有界等待内未确认 run id（runID=="" 且 note 非空）→ 退 1，错误信息即 note，不打印句柄。
func TestRunDetachedWithUnconfirmed(t *testing.T) {
	cmd, out := newDetachTestCmd()
	note := "已发射运行，但未能在超时内确认 run id（子进程仍在运行）。请到运行列表核对。"
	err := runDetachedWith(cmd, &fakeDetachLauncher{note: note}, "flow", "需求", "/proj", false)
	if err == nil || err.Error() != note {
		t.Fatalf("未确认应返回 note 文本错误，得到 %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("未确认不应打印句柄，得到 %q", out.String())
	}
}

// 成功 --json → 退 0，stdout 恰为单行句柄 {"id","workflow"}——契约核心：无 status 字段、恰两个键。
func TestRunDetachedWithSuccessJSON(t *testing.T) {
	cmd, out := newDetachTestCmd()
	fake := &fakeDetachLauncher{runID: "flow-20260707-120000"}
	if err := runDetachedWith(cmd, fake, "flow", "需求", "/proj", true); err != nil {
		t.Fatalf("成功不应报错: %v", err)
	}
	line := strings.TrimSpace(out.String())
	var handle map[string]any
	if err := json.Unmarshal([]byte(line), &handle); err != nil {
		t.Fatalf("句柄非合法 JSON: %v（原文 %q）", err, line)
	}
	if _, hasStatus := handle["status"]; hasStatus {
		t.Fatalf("句柄不应含 status 字段（可寻址句柄非状态快照），得到 %q", line)
	}
	if handle["id"] != "flow-20260707-120000" || handle["workflow"] != "flow" || len(handle) != 2 {
		t.Fatalf("句柄应恰为 {id, workflow}，得到 %q", line)
	}
	if fake.gotName != "flow" || fake.gotPrompt != "需求" || fake.gotCwd != "/proj" {
		t.Fatalf("发射器入参应原样透传：name=%q prompt=%q cwd=%q", fake.gotName, fake.gotPrompt, fake.gotCwd)
	}
}

// 成功人读（默认）→ 退 0，stdout 打印含 run id 的引导提示。
func TestRunDetachedWithSuccessHuman(t *testing.T) {
	cmd, out := newDetachTestCmd()
	if err := runDetachedWith(cmd, &fakeDetachLauncher{runID: "flow-20260707-120000"}, "flow", "需求", "/proj", false); err != nil {
		t.Fatalf("成功不应报错: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "flow-20260707-120000") || !strings.Contains(s, "已在后台启动") {
		t.Fatalf("人读输出应含 run id 与引导提示，得到 %q", s)
	}
}
