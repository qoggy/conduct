---
name: changelog
description: 当要写 changelog、更新版本日志、整理版本变更记录时使用。
---

## Core Task

你是一位资深的专业的软件开发者。你的任务是基于代码变更或项目需求，创建或更新符合 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 标准的 CHANGELOG.md 文件。

## Execution Steps

1. **了解项目背景**：
    - 读取 README.md 了解项目用途和核心功能
    - 查看其他开发文档（如有）了解项目架构和模块划分

2. **确定版本范围**：
    - 查看 git tags：`git tag --sort=-version:refname`
    - 确定要比较的版本范围（如最新 tag 到 HEAD，或两个 tag 之间）

3. **获取代码变更**：
    - 使用 `git diff <tag1>..<tag2>` 或 `git diff <last-tag>..HEAD` 查看文件变更
    - 使用 `git log <tag1>..<tag2> --oneline` 查看提交历史作为参考

4. **分析变更内容**：
    - 识别变更的功能模块和影响范围
    - 判断变更类型：Added/Changed/Deprecated/Removed/Fixed/Security
    - 提取用户关心的功能变化，忽略技术实现细节

5. **编写或更新 CHANGELOG.md**：
    - 如果文件不存在，使用模板创建
    - 确定新版本号（根据语义化版本规范和变更类型）
    - 将分析的变更内容按类型分类写入新版本区块
    - 添加版本号和当前日期
    - 更新底部的版本比较链接

## Important Instructions

### 核心要求

- 基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 标准
- 新版本在前，旧版本在后（倒序排列）
- 按类型分组：Added/Changed/Deprecated/Removed/Fixed/Security
- 面向用户描述，避免技术实现细节
- 使用语义化版本规范。格式：`[MAJOR.MINOR.PATCH] - yyyy-MM-dd`，示例：`[1.2.3] - 2024-01-15`
- 文件名：`CHANGELOG.md`。文件位置：项目根目录，与 `README.md` 同级

### 常见错误和避免方法

#### ❌ 错误做法：包含过多技术细节

```markdown
## [1.0.2] - 2025-10-07

### Fixed
- Fix Spring AOP proxy timing issue in extension framework
  - Move extension registration from `postProcessAfterInstantiation` to `postProcessAfterInitialization` to ensure Spring AOP proxies are registered
  - Move `@ExtensionInject` processing to `postProcessBeforeInitialization` to avoid proxy field injection issues
  - Use `AopUtils.getTargetClass()` to properly handle proxied beans during registration
  - Add comprehensive tests to verify Spring AOP proxy integration

### Added
- Add test dependencies for Spring AOP proxy testing (spring-tx, spring-jdbc, h2)
```

#### ✅ 正确做法：面向用户的描述

```markdown
## [1.0.2] - 2025-10-07

### Fixed
- Fix Spring AOP integration issues that caused AOP features to be bypassed
    - Fixed issue where `@Transactional`, `@Cacheable`, `@PreAuthorize` and other AOP annotations were not working when called through extension framework
    - Fixed `@ExtensionInject` injection failure in Spring AOP proxied beans (e.g., beans with `@Transactional` methods)
```

### 撤回版本处理

对于因重大 bug 或安全问题而撤回的版本：

```markdown
## [0.0.5] - 2014-12-13 [YANKED]

### Fixed
- 修复了严重的安全漏洞

**注意：此版本因安全问题已被撤回，请勿使用。**
```

## 文件模版

<ChangelogTemplate>
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- 新功能描述

### Changed
- 变更描述

### Deprecated
- 即将弃用的功能

### Removed
- 已移除的功能

### Fixed
- Bug 修复描述

### Security
- 安全性改进

## [1.0.0] - 2024-01-15

### Added
- 初始版本发布
- 核心功能实现

### Fixed
- 修复了登录问题

## [0.1.0] - 2024-01-01

### Added
- 项目初始化
- 基础框架搭建

[unreleased]: https://github.com/username/project/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/username/project/compare/v0.1.0...v1.0.0
[0.1.0]: https://github.com/username/project/releases/tag/v0.1.0
</ChangelogTemplate>