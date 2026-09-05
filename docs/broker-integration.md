# Reasonix Broker 集成补丁说明

本文件描述本 fork（branch `broker-refresh`）相对上游 `esengine/Reasonix` `main-v2`
仍保留的补丁、保留理由、退出条件与验证命令。Broker（local-agent-bridge 主仓）是
这些补丁的唯一消费者；上游演进后按本文逐项复核，能满足退出条件即删除对应补丁。

## 上游基线

| 项 | 值 |
| --- | --- |
| 本次合并的上游 commit | `f12cbdd2f9d767d2add378479007b16d3c8eda04`（upstream/main-v2，2026-09-05，v1.37.0 之后） |
| fork 原始 base | `471d62a2d9d2a6c191bf8a5a067d779da1da7c72` |
| 合并方式 | `git merge upstream/main-v2`（保留双方发布历史，无 force/reset） |
| 合并前分支头 | `0c9ade481be04c494936fd7440e01c01aadb08f2` |

## Broker 调用契约（主仓事实源）

- 启动参数（`broker/src/acp/headless/launch/reasonixLaunch.ts`）：
  `reasonix acp --broker-managed --tool-access=<allow|read-only|deny> --planner=off --workspace-only`
- 环境：`REASONIX_HOME` 指向 Broker 管理的隔离 home（`reasonix-home`），
  `REASONIX_CREDENTIALS_STORE=file`；Broker 直接写该 home 下的 `config.toml`
  （唯一 provider `opencode-go`）与 `.env`（`OPENCODE_GO_API_KEY`）。
- 遥测：Broker 订阅 `_reasonix.io/session/status_update`（上游原生通知），读取
  `status.usage.turn.contextPromptTokens/contextCompletionTokens` 合成 `usage_update`；
  同时直接消费 backend 在会话打开时下发的 `usage_update` session update
  （`broker/src/broker/events.ts` → `usage_updated`）。
- 能力声明：`delegationBackendCapabilities.ts` 记录
  “Reasonix internal planner and subagents are disabled; delegation is owned by the broker”。

`REASONIX_HOME` 本身为上游原生机制（配置根解析），本 fork 未改动，不需要补丁。

## 剩余补丁清单

### P1 隔离配置加载：`--broker-managed` + `LoadBrokerManagedForRoot`

- 文件：`internal/config/load.go`、`internal/cli/acp.go`
- 内容：新增 `LoadBrokerManagedForRoot`（只加载 Reasonix 隔离 home 的用户级配置；
  忽略工作区 `reasonix.toml`/`.mcp.json`/插件包/项目 `.env` 展开；从全局凭据库解析
  provider key）。`acpFactory.loadConfig` 与 `SessionConfigState`/`SessionRuntimeState`
  在 `--broker-managed` 下走该入口并跳过全部 legacy 迁移。
- 理由：Broker worker 的工作目录是不可信 checkout；上游 `LoadForRoot` 会合并项目
  配置并在磁盘上执行迁移，允许项目文件改变 worker 运行时。
- 退出条件：上游提供等价的“仅受信 home”加载入口（如 `LoadUserConfigReadOnly` 支持
  凭据解析与 ACP 会话装配）时改用上游实现。
- 测试：`go test ./internal/config/ -run TestLoadBrokerManagedForRootIgnoresProjectConfiguration`
  与 `go test ./internal/cli/ -run TestACPBrokerManagedFactoryIgnoresProjectCommandsAndConfig`

### P2 Broker 管理的运行时隔离（boot 装配面）

- 文件：`internal/boot/boot.go`
- 内容：`boot.Options.BrokerManaged` 为真时：跳过所有 legacy 迁移；技能发现只保留
  隔离 home（清空 ProjectRoot/CustomPaths/Plugin 路径，Broker 经
  `reasonixSkillsDirectory()` 安装的技能仍然生效）；不加载项目 hooks 与 slash
  commands（`ReuseAssembly` 复用路径同样被短路）。
- 理由：与 P1 同源——项目拥有的扩展发现面（hooks 可执行任意命令、commands/skills
  可注入提示）不得进入被监督的 worker。
- 退出条件：上游为受监督 ACP worker 提供官方的 discovery 收紧开关。
- 测试：`go test ./internal/cli/ -run TestACPBrokerManagedFactoryIgnoresProjectCommandsAndConfig`
  （覆盖 config/commands 隔离）；`go test ./internal/boot/`（全包回归装配路径）。

### P3 工具上限：`--tool-access=allow|read-only|deny`

- 文件：`internal/cli/acp.go`、`internal/boot/boot.go`
- 内容：`read-only` 将 executor registry 过滤为 `agent.FilterReadOnlyRegistry` 并用
  `agent.NewReadOnlyAgent`（均为上游原生 API）；`deny` 使用空 registry、丢弃
  `ExtraPlugins`（MCP），并在系统提示末尾追加 Broker tool policy 文本；deny 同时
  抑制技能调用策略块。`--tool-access` 非 `allow` 时要求 `--broker-managed`。
- 理由：Broker 委派的只读/无工具子任务需要进程内硬上限，而不是仅靠提示约束。
- 退出条件：上游 ACP 提供等价的 supervisor 工具面裁剪入口，或 Broker 改为完全
  依赖 MCP 侧裁剪。
- 测试：`go test ./internal/cli/ -run 'TestACPRejectsInvalidSupervisorFlags|TestACPBrokerManagedBootOptionsEnforceToolPolicy'`
  与 `go test ./internal/boot/ -run TestApplyBrokerManagedToolPolicy`

### P4 planner/subagent 关闭（上游 ablation 开关，非模块定制）

- 文件：`internal/cli/acp.go`（`ablationSet`）
- 内容：`--broker-managed` 时经上游 `ablation` 模块禁用 `Planner` 与 `Subagent`；
  `--planner=off` 逻辑保留。上游原生 subagent 模块（`internal/agent/task.go` 等）
  本 fork 零改动——这是对上游公开开关的使用，不是本地 subagent 定制。
- 理由：主仓 `delegationBackendCapabilities.ts` 明确记录该契约（内部 planner 与
  subagent 禁用、委派只归 Broker），需要运行时强制而不是仅提示。
- 退出条件：主仓撤销该能力声明，或上游提供独立于 benchmark ablation 的监督开关。
- 测试：`go test ./internal/cli/ -run 'TestACPSupervisor|TestACP'`（含 flag 校验与
  boot options 断言）

### P5 ACP 状态遥测的上下文占用字段

- 文件：`internal/acp/status_usage.go`、`internal/acp/status.go`
- 内容：`ReasonixUsage` wire 结构与 `usageAccumulator`/持久化遥测增加
  `contextPromptTokens`/`contextCompletionTokens`（最近一次请求的 gauge，非累计
  计费值；provider 未填 Context* 时回落到本次 prompt/completion）。
- 理由：上游 `provider.Usage` 已有 Context* 字段，但 ACP 状态通知不外显；Broker
  依赖 `status.usage.turn.context*` 推导上下文水位（合成 usage_update）。
- 退出条件：上游 `_reasonix.io/session/status_update` 的 usage schema 原生携带
  latest-request context gauge 后删除。
- 测试：`go test ./internal/acp/ -run 'TestStatusExtensionTracksMultipleSessionsAndUsage|TestStatusSnapshotSurvivesSessionResume|TestUsageAccumulator'`

### P6 会话打开时的 `usage_update`（usage 恢复）

- 文件：`internal/acp/protocol.go`、`internal/acp/service.go`
- 内容：`session/load`、`session/resume`（含热会话分支）在 replay 后调用
  `emitUsageSnapshot`：优先用累计 context gauge，缺省回落到
  `Controller.ContextSnapshot()`（上游原生方法；保留接口断言以兼容测试替身），
  发送 `{sessionUpdate:"usage_update", used, size}`。
- 理由：Broker 客户端重连/恢复会话时需要立即恢复上下文占用显示，上游 ACP 无此
  session update。
- 退出条件：ACP 或上游标准化“会话恢复时的 usage/上下文快照”通知后删除。
- 测试：`go test ./internal/acp/ -run 'TestE2ESessionLoad|TestE2ESessionListResumeAndDelete'`
  （断言恰好一条 `usage_update`，used=100 size=200000）

## 本次升级中已收敛/确认不需要本地补丁的项

- `REASONIX_HOME`：上游原生。
- `Controller.ContextSnapshot()`：上游已成为具体方法（`internal/control/controller.go`），
  本地保留的接口断言仅为测试替身兼容。
- `provider.Usage.Context*`：上游原生（`internal/agent/usage_context.go` 的
  `applyLatestContextShape`），P5 只做 wire 外显。
- `ablation.Subagent`：上游原生开关。
- 本地 subagent 模块定制：经 `git diff 471d62a2d..HEAD` 核实不存在（fork 从未改过
  `internal/agent/task.go` 等 subagent 实现），无需删除。
- 旧的 `skill.ApplyIndex`/tokenEconomy 相关本地分支随上游技能装配重构自然消失。

## 构建与验证

工具链：Go ≥1.26（主仓 `scripts/stage-backend-artifacts.mjs` 固定 `go@1.26.5`，
可用 `mise exec go@1.26.5 -- go` 或绝对路径；根分区紧张时把 `GOCACHE`/`TMPDIR`
指到大盘）。

```sh
go build ./...
go test ./internal/config/ ./internal/acp/ ./internal/boot/
go test ./internal/cli/ -run 'TestACP'        # ACP 面
```

已知环境性失败：`internal/cli` 中 6 个 TUI 渲染测试
（`TestIngestEventFlushesAnswer`、`TestClipboardImagePasteKeepsNoticeForRealFailures`、
`TestMiddleClickUsesPrimarySelectionOutsideTmux`、`TestViewMouseModeFollowsCapture`、
`TestViewAltScreenFillsHeight`、`TestParallelBashMarkersKeepOwnLineCount`）在纯
上游 `f12cbdd2f` 同样失败（无 TTY 环境），与本 fork 补丁无关。
