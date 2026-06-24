---
name: litellm-cli-plan
description: LiteLLM CLI 工具实现计划
status: active
date: 2026-06-24
origin: docs/brainstorms/litellm-cli-requirements.md
---

# Plan: litellm-cli 实现计划

## 1. 项目概述

**项目名称**: litellm-cli  
**类型**: Go CLI 工具  
**目标**: 在终端快速查看 LiteLLM API 用量统计和调用日志  
**技术栈**: Go + Cobra + tview (TUI)

---

## 2. 背景

LiteLLM 提供了丰富的用量统计 API，但后台操作需要浏览器访问。通过 CLI 工具可以在终端快速查看，提升开发效率。

**确认的可用 API**:
- `/user/daily/activity` - 用户每日用量统计
- `/team/daily/activity` - 团队每日用量统计
- `/spend/logs` - 消费日志 (v1)
- `/models` - 模型列表

**已知限制**:
- `/spend/logs/v2` 需要 admin 权限，暂不可用
- LiteLLM 无真正的流式接口，用轮询 + TUI 模拟伪流式

---

## 3. 功能需求

### 3.1 stats 命令 - 用量统计

| 功能 | 说明 |
|------|------|
| 日/周/月汇总 | 查看指定时间范围的总量统计 |
| 维度筛选 | 支持按 api_key、team_id、model 筛选 |
| 聚合视图 | 支持按 key、team、model 分组 |
| 输出字段 | 请求数、prompt tokens、completion tokens、总花费 |

### 3.2 logs 命令 - 消费日志

| 功能 | 说明 |
|------|------|
| 轮询模式 | 定期重新请求，模拟流式更新 |
| 过滤能力 | 按 model、status 过滤 |
| TUI 显示 | 表格形式展示时间、模型、状态、tokens |
| 刷新间隔 | 默认 5 秒，可配置 |

### 3.3 辅助命令

| 命令 | 说明 |
|------|------|
| models | 查看可用模型列表 |
| key info | 查看当前 Key 详情 |

### 3.4 配置管理

- 环境变量: `LITELLM_API_KEY`, `LITELLM_BASE_URL`
- 配置文件: `~/.litellm-cli.yaml`

---

## 4. 技术设计

### 4.1 架构

```
litellm-cli/
├── cmd/
│   ├── root.go       # 根命令
│   ├── stats.go      # stats 子命令
│   ├── logs.go       # logs 子命令
│   └── models.go     # models 子命令
├── internal/
│   ├── config/       # 配置管理
│   ├── client/       # API 客户端
│   ├── api/          # API 响应结构
│   └── ui/           # TUI 组件
└── main.go
```

### 4.2 依赖

| 依赖 | 用途 |
|------|------|
| cobra | CLI 框架 |
| tview | TUI 界面 |
| tcell | TUI 底层 |
| viper | 配置管理 |
| resty | HTTP 客户端 |

### 4.3 API 响应结构

根据测试结果:

**/user/daily/activity 返回**:
```json
{
  "results": [
    {
      "date": "2026-06-24",
      "metrics": {
        "spend": 0.39,
        "prompt_tokens": 4336613,
        "completion_tokens": 49900,
        "total_tokens": 4386513,
        "successful_requests": 147,
        "failed_requests": 7,
        "api_requests": 154
      },
      "breakdown": {
        "models": {...},
        "api_keys": {...}
      }
    }
  ]
}
```

**/spend/logs 返回** (v1, 非逐条日志):
- 返回按 key 分组的聚合数据，不是逐条请求日志
- 这是已知限制，可能无法实现理想的 logs 功能

---

## 5. 实现单元

### U1. 项目初始化

**目标**: 创建 Go 项目结构，配置依赖

**Files**:
- `go.mod`
- `main.go`
- `cmd/root.go`

**Approach**:
- 使用 cobra 初始化项目
- 配置 viper 读取环境变量和配置文件

**Test scenarios**:
- `go build` 成功
- `--help` 输出正确

---

### U2. 配置管理

**目标**: 统一管理 API Key 和 Base URL

**Files**:
- `internal/config/config.go`

**Approach**:
- 优先级: CLI flag > 环境变量 > 配置文件 > 默认值
- 默认 Base URL: `http://localhost:4000`

**Test scenarios**:
- 环境变量配置生效
- 配置文件读取成功
- 缺少必需配置时提示错误

---

### U3. API 客户端

**目标**: 封装 HTTP 请求

**Files**:
- `internal/client/client.go`
- `internal/api/types.go`

**Approach**:
- 使用 resty 发送请求
- 添加 Authorization header
- 统一错误处理

**Test scenarios**:
- 正常请求返回数据
- 401 错误提示认证失败
- 网络错误提示连接问题

---

### U4. stats 命令实现

**目标**: 用量统计功能

**Files**:
- `cmd/stats.go`
- `internal/api/stats.go`

**Approach**:
- 调用 `/user/daily/activity` 和 `/team/daily/activity`
- 支持 `--period` (day/week/month) 参数
- 支持 `--by` (key/team/model) 聚合
- 使用表格输出 (tview Table)

**Test scenarios**:
- `stats` 显示今日用量
- `stats --period week` 显示本周用量
- `stats --by team` 按团队分组
- 无权限时提示错误

---

### U5. logs 命令实现 (TUI)

**目标**: 轮询式伪流式日志查看

**Files**:
- `cmd/logs.go`
- `internal/ui/logs.go`

**Approach**:
- 调用 `/spend/logs` 定期轮询
- 使用 tview 构建 TUI
- 支持 `--interval` 刷新间隔
- 支持 `--model` 过滤
- Ctrl+C 正常退出

**注意**: v1 接口返回聚合数据，不是逐条日志。此实现可能需要根据实际数据调整。

**Test scenarios**:
- `logs` 启动 TUI
- `logs --interval 10` 10 秒刷新
- `logs --model gpt-4` 过滤模型
- Ctrl+C 退出

---

### U6. models 命令实现

**目标**: 查看可用模型列表

**Files**:
- `cmd/models.go`

**Approach**:
- 调用 `/models` 获取模型列表
- 简单表格输出

**Test scenarios**:
- `models` 显示模型列表

---

## 6. 成功标准

1. `litellm-cli stats` 正确显示用量统计
2. `litellm-cli logs` 启动 TUI 并轮询更新
3. 支持环境变量和配置文件
4. 错误信息清晰
5. 启动速度 < 1 秒

---

## 7. 风险与限制

| 风险 | 影响 | 缓解 |
|------|------|------|
| /spend/logs v1 数据格式限制 | logs 可能无法显示逐条请求 | 根据实际数据调整输出 |
| 需要 admin 权限的接口 | 部分功能不可用 | 文档说明 |
| TUI 兼容性 | 终端环境差异 | 测试主流终端 |

---

## 8. 下一步

实现按 U1 → U6 顺序进行。