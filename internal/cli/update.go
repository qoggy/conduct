package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/creativeprojects/go-selfupdate"
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
	)
	cmd := &cobra.Command{
		Use:   localizedHelpText("update [版本]", "update [version]"),
		Short: localizedHelpText("自更新 conduct 到最新版本（或指定版本）", "Update conduct to the latest version (or a specified version)"),
		Long: localizedHelpText(`自更新 conduct：从 GitHub Releases 下载匹配本机系统/架构的预编译二进制，
校验 checksum 后原地替换当前正在运行的可执行文件。无需本机安装 Go 工具链。

用法：
  conduct update                更新到最新正式版
  conduct update v0.2.0         更新（或回退）到指定版本；显式版本号可安装预发布版
  conduct update --pre          更新到最新版本，纳入预发布（beta）版本
  conduct update --check        只检查有无新版本，不实际安装

说明：
  · 若 conduct 由 Homebrew 安装，请改用对应的包管理器命令（如 brew upgrade
    conduct），自更新会拒绝执行以免与包管理器冲突。
  · 尚无任何 Release 时命令会明确报错，而非静默无动作。`, `Update conduct by downloading a precompiled binary matching the local system/architecture from GitHub Releases,
verifying its checksum, and replacing the currently running executable in place. The Go toolchain is not required locally.

Usage:
  conduct update                Update to the latest stable release
  conduct update v0.2.0         Update (or roll back) to the specified version; an explicit version may install a prerelease
  conduct update --pre          Update to the latest version, including prerelease (beta) versions
  conduct update --check        Only check whether a new version exists; do not install it

Notes:
  · If conduct was installed by Homebrew, use the corresponding package-manager command instead (such as brew upgrade
    conduct); self-update refuses to run to avoid conflicting with the package manager.
  · If no Release exists yet, the command reports an explicit error instead of silently doing nothing.`),
		Args:          maximumArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runUpdate(cmd, target, checkOnly, pre)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, localizedHelpText("只检查有无新版本，不实际安装", "Only check for a new version; do not install it"))
	cmd.Flags().BoolVar(&pre, "pre", false, localizedHelpText("纳入预发布（beta）版本", "Include prerelease (beta) versions"))
	return cmd
}

// runUpdate 执行一次自更新（或检查）。checkOnly 为真时只比对版本、不安装；
// pre 为真时把预发布版本纳入「最新」的候选。
func runUpdate(cmd *cobra.Command, target string, checkOnly, pre bool) error {
	out := cmd.OutOrStdout()

	// 定位当前可执行文件的真实路径：解开软链，因为 UpdateTo 要按真实路径做原子替换；
	// 且 Homebrew 会把 /usr/local/bin/conduct 软链到 Cellar，解链后才能识别出 brew 安装。
	exePath, err := currentExecutablePath()
	if err != nil {
		return fmt.Errorf("failed to locate current executable: %w", err)
	}

	// 经 Homebrew 安装时拒绝自替换——否则会与包管理器的版本记账打架、且下次 brew
	// 操作会覆盖掉替换结果。fail-loud 引导用户走 brew。
	if prefix := homebrewPrefixOf(exePath); prefix != "" {
		return localizedErrorf("检测到 conduct 由 Homebrew 安装（%s）；请改用 `brew upgrade conduct` 更新，自更新不接管包管理器管辖的二进制", "conduct appears to be installed by Homebrew (%s); use `brew upgrade conduct` because self-update does not take over package-manager-managed binaries", exePath)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		// GoReleaser 默认产出单一 checksums.txt，一行一个资产的 SHA256。
		Validator:  &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		Prerelease: pre,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize updater: %w", err)
	}
	repo := selfupdate.ParseSlug(updateRepoSlug)
	ctx := context.Background()

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
		return fmt.Errorf("failed to query releases: %w", err)
	}
	if !found {
		if target != "" {
			return localizedErrorf("未找到版本 %q 的发布，或该发布无匹配 %s/%s 的资产", "release %q was not found, or it has no matching %s/%s asset", target, runtime.GOOS, runtime.GOARCH)
		}
		return localizedErrorf("尚无可用发布：%s 还没有正式 Release，或无匹配 %s/%s 的资产", "no release is available: %s has no stable release or no matching %s/%s asset", updateRepoSlug, runtime.GOOS, runtime.GOARCH)
	}

	// 与当前版本比较。当前版本可能是 dev / git 伪版本 / dirty 等非规范 semver，
	// 这类情况无法可靠比大小，跳过「已最新」判断、如实说明，绝不假装能算落后几个版本。
	cur := strings.TrimPrefix(version, "v")
	curIsSemver := isSemanticVersion(cur)
	upToDate := curIsSemver && release.LessOrEqual(cur)

	if checkOnly {
		switch {
		case upToDate:
			fmt.Fprintf(out, localizedHelpText("已是最新：当前 %s，最新发布 %s\n", "Up to date: current %s, latest release %s\n"), version, release.Version())
		case curIsSemver:
			fmt.Fprintf(out, localizedHelpText("有新版本：当前 %s → 可更新到 %s\n运行 `conduct update` 安装。\n", "A new version is available: current %s → %s\nRun `conduct update` to install it.\n"), version, release.Version())
		default:
			fmt.Fprintf(out, localizedHelpText("当前版本 %s 非规范版本号，无法比较；最新发布为 %s\n运行 `conduct update` 安装。\n", "Current version %s is not a canonical version and cannot be compared; the latest release is %s.\nRun `conduct update` to install it.\n"), version, release.Version())
		}
		return nil
	}

	if upToDate {
		fmt.Fprintf(out, localizedHelpText("已是最新版本 %s，无需更新。\n", "Version %s is already the latest; no update is needed.\n"), version)
		return nil
	}

	fmt.Fprintf(out, localizedHelpText("正在更新：%s → %s …\n", "Updating: %s → %s …\n"), version, release.Version())
	if err := updater.UpdateTo(ctx, release, exePath); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	fmt.Fprintf(out, localizedHelpText("已更新到 %s（安装于 %s）\n", "Updated to %s (installed at %s)\n"), release.Version(), exePath)
	if release.URL != "" {
		fmt.Fprintf(out, localizedHelpText("发布说明：%s\n", "Release notes: %s\n"), release.URL)
	}
	return nil
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
