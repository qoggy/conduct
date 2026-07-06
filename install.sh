#!/bin/sh
# conduct 安装脚本 —— 下载匹配本机系统/架构的预编译二进制并安装到 PATH。
#
#   curl -sSL https://raw.githubusercontent.com/qoggy/conduct/main/install.sh | sh
#
# 可用环境变量：
#   CONDUCT_VERSION      指定版本 tag（如 v0.2.0）；默认取最新正式版
#   CONDUCT_INSTALL_DIR  安装目录；默认 /usr/local/bin（不可写则回退 $HOME/.local/bin）
#
# 装好后可用 `conduct update` 自更新，无需再跑本脚本。
set -eu

REPO="qoggy/conduct"
BINARY="conduct"

info() { printf '%s\n' "$*" >&2; }
err() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}
need() { command -v "$1" >/dev/null 2>&1 || err "缺少依赖命令：$1"; }

# 下载器：优先 curl，回退 wget。
download() { # download <url> <dest>
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		err "需要 curl 或 wget"
	fi
}

need tar

# 探测 OS。
os="$(uname -s)"
case "$os" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*) err "暂不支持的操作系统：$os（当前仅发布 darwin / linux 预编译版；可用 go install 从源码安装）" ;;
esac

# 探测架构。
arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*) err "暂不支持的架构：$arch（当前仅发布 amd64 / arm64）" ;;
esac

# 解析目标版本 tag：显式 CONDUCT_VERSION 优先，否则查最新正式版。
tag="${CONDUCT_VERSION:-}"
if [ -z "$tag" ]; then
	info "查询最新版本 …"
	api="https://api.github.com/repos/${REPO}/releases/latest"
	tag="$(download "$api" /dev/stdout | sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -n 1)"
	[ -n "$tag" ] || err "无法解析最新版本；可设 CONDUCT_VERSION 指定，或稍后重试（GitHub API 可能限流）"
fi

asset="${BINARY}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

info "下载 ${asset}（${tag}）…"
download "${base}/${asset}" "${tmp}/${asset}" || err "下载失败：${base}/${asset}"

# 校验 checksum（有 checksums.txt 才校验；缺失则跳过并提示，不静默）。
if download "${base}/checksums.txt" "${tmp}/checksums.txt" 2>/dev/null; then
	info "校验 checksum …"
	expected="$(sed -n "s/  *${asset}\$//p; s/^\\([0-9a-f]\\{64\\}\\)  *${asset}\$/\\1/p" "${tmp}/checksums.txt" | grep -E '^[0-9a-f]{64}$' | head -n 1)"
	if [ -n "$expected" ]; then
		if command -v sha256sum >/dev/null 2>&1; then
			actual="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
		elif command -v shasum >/dev/null 2>&1; then
			actual="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
		else
			actual=""
			info "警告：无 sha256sum / shasum，跳过校验"
		fi
		if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
			err "checksum 不匹配：期望 $expected，实得 $actual"
		fi
	else
		info "警告：checksums.txt 未含 ${asset} 条目，跳过校验"
	fi
else
	info "警告：未获取到 checksums.txt，跳过校验"
fi

info "解包 …"
tar -xzf "${tmp}/${asset}" -C "${tmp}"
[ -f "${tmp}/${BINARY}" ] || err "归档中未找到可执行文件 ${BINARY}"
chmod +x "${tmp}/${BINARY}"

# 选择安装目录：CONDUCT_INSTALL_DIR > /usr/local/bin（可写）> $HOME/.local/bin。
dir="${CONDUCT_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ] 2>/dev/null; then
		dir="/usr/local/bin"
	else
		dir="${HOME}/.local/bin"
	fi
fi
mkdir -p "$dir"

if mv "${tmp}/${BINARY}" "${dir}/${BINARY}" 2>/dev/null; then
	:
elif command -v sudo >/dev/null 2>&1; then
	info "${dir} 需提权写入，使用 sudo …"
	sudo mv "${tmp}/${BINARY}" "${dir}/${BINARY}"
else
	err "无法写入 ${dir}；请设 CONDUCT_INSTALL_DIR 为可写目录后重试"
fi

info ""
info "已安装 ${BINARY} ${tag} → ${dir}/${BINARY}"
case ":${PATH}:" in
*":${dir}:"*) ;;
*) info "注意：${dir} 不在 PATH 中，请将其加入 PATH（如 export PATH=\"${dir}:\$PATH\"）" ;;
esac
info "运行 \`${BINARY} version\` 验证，\`${BINARY} update\` 自更新。"
