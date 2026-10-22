package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
)

// updateRepoSlug 是自更新拉取发布的 GitHub 仓库（owner/repo）。
const updateRepoSlug = "qoggy/conduct"

// newUpdateCommand 构造 `conduct update`：从 GitHub Releases 下载匹配本机
// GOOS/GOARCH 的预编译二进制、校验 checksum 后原地热替换当前可执行文件。
//
// 更新机制镜像分发机制——conduct 经预编译 Release 分发（GoReleaser），故自更新
// 是「下二进制、校验 checksum、原子替换」，而非重新编译。因此无需本机装 Go 工具链。
func newUpdateCommand() *cobra.Command {
	var (
		checkOnly bool
		pre       bool
		project   bool
		global    bool
	)
	cmd := &cobra.Command{
		Use:   localizedHelpText("update [版本]", "update [version]"),
		Short: localizedHelpText("更新 conduct 和已安装的 Skill", "Update conduct and installed Skills"),
		Long: localizedHelpText(`更新 conduct，并更新通过 conduct init 安装的 Skill。
默认更新到最新正式版，也可指定版本（包括预发布版）。
默认更新所有安装位置；--project / --global 仅限定 Skill 范围，两者不能同时使用。
conduct 已是最新版本时仍会更新 Skill；--check 仅检查 conduct 版本，不执行更新。

示例：
  conduct update                更新 conduct 和所有已安装的 Skill
  conduct update --project      更新 conduct 和当前目录的 Skill
  conduct update v0.2.0         更新到指定版本
  conduct update --check        仅检查 conduct 更新`, `Update conduct and Skills installed with conduct init.
Use the latest stable release by default, or specify a version (including prereleases).
All installation locations are updated by default. --project / --global restrict only Skill updates and cannot be combined.
Skills are updated even when conduct is up to date. --check only checks for a conduct update without making changes.

Examples:
  conduct update                Update conduct and all installed Skills
  conduct update --project      Update conduct and Skills in the current directory
  conduct update v0.2.0         Update to a specified version
  conduct update --check        Only check for a conduct update`),
		Args:          maximumArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			if project && global {
				return scopeUsageError()
			}
			scope := skillinstall.Scope("")
			if project {
				scope = skillinstall.Project
			}
			if global {
				scope = skillinstall.Global
			}
			return runUpdateWithDependencies(cmd, target, checkOnly, pre, scope, defaultUpdateDependencies())
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, localizedHelpText("仅检查 conduct 更新，不修改文件", "Only check for a conduct update without changing files"))
	cmd.Flags().BoolVar(&pre, "pre", false, localizedHelpText("检查最新版本时包含预发布版", "Include prereleases when checking for the latest version"))
	cmd.Flags().BoolVar(&project, "project", false, localizedHelpText("仅更新当前目录的 Skill", "Update Skills in the current directory only"))
	cmd.Flags().BoolVar(&global, "global", false, localizedHelpText("仅更新用户级 Skill", "Update user-level Skills only"))
	return cmd
}

func runUpdateWithDependencies(cmd *cobra.Command, target string, checkOnly, pre bool, scope skillinstall.Scope, dependencies updateDependencies) error {
	out := cmd.OutOrStdout()

	// 定位当前可执行文件的真实路径：解开软链，因为 UpdateTo 要按真实路径做原子替换；
	// 且 Homebrew 会把 /usr/local/bin/conduct 软链到 Cellar，解链后才能识别出 brew 安装。
	exePath, err := dependencies.executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable: %w", err)
	}

	// 经 Homebrew 安装时拒绝自替换——否则会与包管理器的版本记账打架、且下次 brew
	// 操作会覆盖掉替换结果。fail-loud 引导用户走 brew。
	if prefix := homebrewPrefixOf(exePath); prefix != "" {
		return localizedErrorf("conduct 由 Homebrew 管理，请运行 `brew upgrade conduct` 更新。", "conduct is managed by Homebrew. Run `brew upgrade conduct` to update.")
	}

	candidate, err := dependencies.query(cmd.Context(), target, pre)
	if err != nil {
		return err
	}

	// 与当前版本比较。当前版本可能是 dev / git 伪版本 / dirty 等非规范 semver，
	// 这类情况无法可靠比大小，跳过「已最新」判断、如实说明，绝不假装能算落后几个版本。
	cur := strings.TrimPrefix(version, "v")
	curIsSemver := isSemanticVersion(cur)
	upToDate := false
	if curIsSemver {
		currentVersion, parseErr := semver.NewVersion(cur)
		if parseErr != nil {
			return fmt.Errorf("parse current version: %w", parseErr)
		}
		releaseVersion, parseErr := semver.NewVersion(candidate.Version)
		if parseErr != nil {
			return fmt.Errorf("parse release version: %w", parseErr)
		}
		upToDate = releaseVersion.Compare(currentVersion) <= 0
	}

	if checkOnly {
		switch {
		case upToDate:
			if _, err := fmt.Fprintf(out, localizedHelpText("conduct 已是最新版本 %s（最新发布 %s）。\n", "conduct %s is up to date (latest release: %s).\n"), version, candidate.Version); err != nil {
				return err
			}
		case curIsSemver:
			if _, err := fmt.Fprintf(out, localizedHelpText("conduct 有新版本：%s → %s。\n运行 `conduct update`。\n", "A new conduct version is available: %s → %s.\nRun `conduct update`.\n"), version, candidate.Version); err != nil {
				return err
			}
		default:
			if _, err := fmt.Fprintf(out, localizedHelpText("无法比较当前 conduct 版本 %s；最新发布为 %s。\n运行 `conduct update`。\n", "Current conduct version %s cannot be compared; the latest release is %s.\nRun `conduct update`.\n"), version, candidate.Version); err != nil {
				return err
			}
		}
		return nil
	}

	if upToDate {
		if _, err := fmt.Fprintf(out, localizedHelpText("conduct 已是最新版本 %s。\n", "conduct %s is up to date.\n"), version); err != nil {
			return err
		}
		return dependencies.synchronize(cmd, scope)
	}

	if _, err := fmt.Fprintf(out, localizedHelpText("正在更新 conduct：%s → %s …\n", "Updating conduct: %s → %s …\n"), version, candidate.Version); err != nil {
		return err
	}
	if err := candidate.Install(cmd.Context(), exePath); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	if _, err := fmt.Fprintf(out, localizedHelpText("conduct 已更新至 %s。\n", "conduct updated to %s.\n"), candidate.Version); err != nil {
		return err
	}
	if candidate.URL != "" {
		if _, err := fmt.Fprintf(out, localizedHelpText("发布说明：%s\n", "Release notes: %s\n"), candidate.URL); err != nil {
			return err
		}
	}
	return dependencies.handoff(cmd, exePath, candidate.Version, scope)
}

type updateCandidate struct {
	Version string
	URL     string
	Install func(context.Context, string) error
}

type updateDependencies struct {
	executable  func() (string, error)
	query       func(context.Context, string, bool) (*updateCandidate, error)
	synchronize func(*cobra.Command, skillinstall.Scope) error
	handoff     func(*cobra.Command, string, string, skillinstall.Scope) error
}

func defaultUpdateDependencies() updateDependencies {
	return updateDependencies{executable: currentExecutablePath, query: queryUpdateCandidate, synchronize: syncRegisteredSkills, handoff: handoffSkillSync}
}

func queryUpdateCandidate(ctx context.Context, target string, pre bool) (*updateCandidate, error) {
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		// GoReleaser 默认产出单一 checksums.txt，一行一个资产的 SHA256。
		Validator:  &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		Prerelease: pre,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize updater: %w", err)
	}
	repo := selfupdate.ParseSlug(updateRepoSlug)

	var (
		release *selfupdate.Release
		found   bool
	)
	if target != "" {
		// 显式版本走 DetectVersion——它能命中预发布版本，是 opt-in beta 的入口。
		release, found, err = updater.DetectVersion(ctx, repo, strings.TrimPrefix(target, "v"))
	} else {
		release, found, err = updater.DetectLatest(ctx, repo)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query releases: %w", err)
	}
	if !found {
		if target != "" {
			return nil, localizedErrorf("未找到版本 %q 的发布，或该发布无匹配 %s/%s 的资产", "release %q was not found, or it has no matching %s/%s asset", target, runtime.GOOS, runtime.GOARCH)
		}
		return nil, localizedErrorf("尚无可用发布：%s 还没有正式 Release，或无匹配 %s/%s 的资产", "no release is available: %s has no stable release or no matching %s/%s asset", updateRepoSlug, runtime.GOOS, runtime.GOARCH)
	}

	return &updateCandidate{Version: release.Version(), URL: release.URL, Install: func(ctx context.Context, path string) error { return updater.UpdateTo(ctx, release, path) }}, nil
}

// currentExecutablePath 返回当前可执行文件的真实路径（尽力解开软链）。
// EvalSymlinks 失败时退回未解链的原路径——这是刻意的降级（原路径仍可用于替换），
// 而非静默吞错：多数平台 os.Executable 已给出可用路径，解链只是锦上添花。
func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// homebrewPrefixOf 判断可执行文件是否落在 Homebrew 安装前缀下；是则返回命中的前缀。
// 覆盖 /opt/homebrew（Apple Silicon）、/usr/local/Cellar（Intel mac）、
// /home/linuxbrew（Linuxbrew）。调用方已先解开软链，故 brew 从 bin 软链到 Cellar
// 的情形也能识别。
func homebrewPrefixOf(exePath string) string {
	prefixes := []string{
		"/opt/homebrew/",
		"/usr/local/Cellar/",
		"/usr/local/Homebrew/",
		"/home/linuxbrew/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(exePath, p) {
			return p
		}
	}
	return ""
}

// semverPattern 匹配规范语义化版本（不含前导 v，前面已剥离）：主.次.修订 + 可选
// 预发布/构建元数据。用于判断当前版本能否参与可靠的大小比较。
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// isSemanticVersion 报告 v 是否为规范语义化版本号。
func isSemanticVersion(v string) bool {
	return semverPattern.MatchString(v)
}
