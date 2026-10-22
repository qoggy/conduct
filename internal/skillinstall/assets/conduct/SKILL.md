---
name: conduct
description: 本机 conduct workflow 编排 CLI 的操作助手，用于把 JSON workflow 解释为节点与边组成的 DAG，并按依赖驱动 AI 编程引擎执行。用户提到 conduct、创建/编辑/查看 workflow、配置节点或 prompt、编排带依赖的 AI 编程任务，或运行、等待、恢复、停止 workflow 时使用。
license: MIT
compatibility: Requires Python 3.7+, Bash, and the conduct CLI in PATH.
---

# Conduct

conduct 是本机 workflow 编排 CLI。它把 JSON workflow 解释为节点与边组成的 DAG，并按依赖关系驱动 AI 编程引擎执行。

## 操作流程

编辑 workflow 前，先一次性读取编辑相关的实时帮助：

```bash
python3 <conduct-skill-dir>/scripts/workflow_help.py
```

运行、查询、等待、恢复或停止 workflow 前，先一次性读取运行相关的实时帮助：

```bash
python3 <conduct-skill-dir>/scripts/run_help.py
```

`<conduct-skill-dir>` 是本 Skill 的实际安装目录。

以脚本输出的本机 `conduct help` 为参数、格式和行为的权威说明，不凭经验猜测 JSON 结构或 CLI 参数。读取帮助后再组合并执行具体命令。

## 重要注意事项

- conduct 运行较慢，单个节点至少需要三分钟。不要轮询运行结果；依据实时帮助使用 wait 能力，并为 Bash 调用预留足够长的等待时间。

## 脚本

- `scripts/workflow_help.py`：汇总 workflow 编辑与管理的核心帮助。
- `scripts/run_help.py`：汇总 workflow 运行、run 查看、列表、等待、恢复和停止相关帮助。
