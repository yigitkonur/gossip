# AgentBridge

AgentBridge 让 **Claude Code** 和 **Codex** 能在同一台机器上协同工作。

用最简单的话说：
- **Claude Code** 负责审阅、规划、质疑方案。
- **Codex** 负责实现和执行。
- **AgentBridge** 是本地桥接层，负责把它们连接起来。

这个项目以前是 TypeScript/Bun 版本。旧版本仍然保留在 `ts-legacy/` 里供参考，但**当前真正运行的是 Go 版本**。

## 系统是怎么工作的

你可以把 AgentBridge 理解成 4 层：

1. **Claude bridge**
   - 当前台启动 `agentbridge claude` 时运行。
   - 通过标准输入输出和 Claude Code 的 MCP 通信。

2. **Daemon**
   - 后台常驻进程。
   - 就算 Claude 前台暂时断开，它也能把系统继续维持住。

3. **Codex proxy**
   - 接收 Codex TUI 的连接。
   - 负责重写请求 ID，保证请求和响应不会串线。

4. **Codex app-server 连接**
   - 通过 WebSocket 连接真正的 `codex app-server`。

所以真正的消息链路是：

**Claude Code → MCP bridge → daemon → Codex proxy → Codex app-server**

然后再返回。

## 你可以运行的命令

当前 CLI 命令是：

- `agentbridge init`
- `agentbridge claude`
- `agentbridge codex`
- `agentbridge kill`
- `agentbridge status`
- `agentbridge version`

### 每个命令的作用

#### `agentbridge init`
在当前项目里创建 `.agentbridge/` 文件夹，并生成初始文件：
- `config.json`
- `collaboration.md`

如果当前项目还没有初始化，先运行它。

#### `agentbridge claude`
启动 Claude 侧的桥接进程。

Claude Code 会通过这个命令和 AgentBridge 进行 MCP 通信。

#### `agentbridge codex`
确保后台 daemon 已启动，等待 proxy 准备完成，然后启动连接到该 proxy 的 Codex TUI。

#### `agentbridge kill`
停止后台 daemon，并写入一个 **killed sentinel** 文件。

这个 sentinel 很重要：它告诉系统**不要自动重连**，直到用户明确重新启动。

#### `agentbridge status`
打印当前 daemon 状态，例如：
- bridge 是否 ready
- TUI 是否已连接
- 当前 thread ID
- 队列数量
- daemon PID

#### `agentbridge version`
打印当前构建版本。

## 运行时最重要的概念

### 1. Ready 状态
只有当 Codex 真的准备好时，Claude 才应该发送回复。

AgentBridge 通过 TUI 状态机和 thread readiness 来判断这一点。如果系统还没 ready，就会直接拒绝回复，而不是静默丢消息。

### 2. 当前 TUI 所有权
只有**当前**的 Codex TUI 连接可以接收上游实时流量。

这样可以避免旧连接或重复连接错误地回复请求。

### 3. 缓冲与重放
如果 Claude 暂时断开，daemon 可以把 Codex 的消息先缓冲起来，等 Claude 重新连上后再重放。

这样短暂断线不会丢掉重要输出。

### 4. Killed sentinel
`agentbridge kill` 会在状态目录里写一个 sentinel 文件。

它的含义是：
- 不要自动重连
- 不要悄悄重启后台流程
- 等用户明确重新启动

### 5. 状态目录
运行时文件会放在共享状态目录里。

在 macOS 上，通常是：
- `~/Library/Application Support/AgentBridge`

里面会有：
- `daemon.pid`
- `daemon.lock`
- `status.json`
- `agentbridge.log`
- `killed`
- `codex-tui.pid`

## 目录导览

下面是适合初学者的目录地图：

- `cmd/agentbridge/` — 真正的 CLI 命令入口
- `internal/protocol/` — 协议类型和方法名
- `internal/jsonrpc/` — 通用 JSON-RPC 引擎
- `internal/codex/` — Codex 子进程、WebSocket、proxy、turn 处理
- `internal/mcp/` — Claude 侧 MCP 服务与工具
- `internal/control/` — daemon 与 Claude bridge 之间的 WebSocket 协议
- `internal/daemon/` — 后台主管理器与生命周期逻辑
- `internal/filter/` — 消息重要性规则与状态摘要
- `internal/tui/` — TUI 就绪状态与断线宽限逻辑
- `internal/statedir/` — 运行时文件路径
- `internal/config/` — 项目本地 `.agentbridge/` 配置
- `schema/` — vendored 的 Codex 协议 schema 快照
- `scripts/` — schema / protocol 维护脚本
- `plugins/agentbridge/` — 当前插件元数据与配置
- `docs/` — 设计说明与架构历史
- `ts-legacy/` — 旧 TypeScript/Bun 实现，仅作参考

## 如何安全开发

推荐的新手工作流：

1. 先读顶层 `AGENTS.md`
2. 再读你要修改目录里的更深层 `AGENTS.md`
3. 先做最小改动
4. 先跑最有针对性的测试
5. 再跑完整检查：

```bash
rtk go test ./...
rtk go vet ./...
rtk go build ./...
```

如果改动涉及发布或打包，还要运行：

```bash
rtk $(go env GOPATH)/bin/goreleaser build --snapshot --clean --single-target
```

## 什么是当前实现，什么是历史参考

### 当前实现
- `cmd/agentbridge/` 中的 Go CLI
- `internal/` 中的 Go 运行时
- `.github/workflows/ci.yml` 中的 Go CI
- `plugins/agentbridge/` 中的当前插件元数据

### 历史参考
- `ts-legacy/`
- 旧的 TypeScript/Bun 脚本
- `ts-legacy/plugins/` 中的旧插件布局
- 仍然描述旧架构的历史文档

## 如果你是第一次看这个仓库

建议按下面顺序阅读：

1. `README.md`
2. `AGENTS.md`
3. `cmd/agentbridge/AGENTS.md`
4. `internal/daemon/AGENTS.md`
5. `internal/codex/AGENTS.md`
6. `internal/mcp/AGENTS.md`

这样你能最快从外到内理解整个系统。
