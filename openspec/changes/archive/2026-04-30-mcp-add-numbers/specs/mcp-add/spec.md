## ADDED Requirements

### Requirement: MCP 服务端进程
系统 SHALL 提供一个独立的 Go 可执行文件，启动后作为符合 Model Context Protocol 规范的服务端，通过标准输入输出（stdio）与 MCP 客户端通信。

#### Scenario: 客户端通过 stdio 拉起服务
- **WHEN** MCP 客户端以子进程方式启动本可执行文件并通过 stdin/stdout 发送 `initialize` 请求
- **THEN** 服务 MUST 返回合法的 MCP `initialize` 响应，声明自身的服务名、版本以及至少一个工具能力

#### Scenario: 客户端关闭连接
- **WHEN** MCP 客户端关闭 stdin 或发送标准的关闭流程
- **THEN** 服务 MUST 在完成未决请求后干净退出，进程退出码为 0

### Requirement: add 工具注册
系统 SHALL 向 MCP 客户端注册一个名为 `add` 的工具，其描述说明"返回两个数值的和"，并声明两个必填数值参数 `a` 与 `b`。

#### Scenario: 列出工具
- **WHEN** 客户端调用 MCP `tools/list`
- **THEN** 响应 MUST 包含一个 `name` 为 `add` 的工具条目，其 `inputSchema` 要求 `a` 与 `b` 两个 `number` 类型的必填参数

### Requirement: 两数相加计算
当工具 `add` 被调用且 `a` 与 `b` 均为合法数值时，系统 SHALL 计算 `a + b` 并以文本内容返回结果。

#### Scenario: 两个整数相加
- **WHEN** 客户端调用 `tools/call` 工具 `add`，参数为 `{"a": 2, "b": 3}`
- **THEN** 响应 MUST 返回一个 `content` 条目，`type` 为 `text`，`text` 字段为字符串 `"5"`
- **AND** 响应 MUST NOT 被标记为错误

#### Scenario: 浮点数相加
- **WHEN** 客户端调用 `add`，参数为 `{"a": 1.5, "b": 2.25}`
- **THEN** 响应文本 MUST 为 `"3.75"`

#### Scenario: 负数相加
- **WHEN** 客户端调用 `add`，参数为 `{"a": -10, "b": 4}`
- **THEN** 响应文本 MUST 为 `"-6"`

#### Scenario: 与零相加
- **WHEN** 客户端调用 `add`，参数为 `{"a": 0, "b": 0}`
- **THEN** 响应文本 MUST 为 `"0"`

### Requirement: 非法输入处理
当 `add` 工具的入参缺失或类型非法时，系统 SHALL 返回一个被标记为错误（`isError: true`）的 MCP 工具响应，且进程 MUST NOT 崩溃。

#### Scenario: 缺少参数 a
- **WHEN** 客户端调用 `add`，参数为 `{"b": 3}`
- **THEN** 响应 MUST 标记为错误，文本内容 MUST 指出参数 `a` 缺失或无效

#### Scenario: 参数类型错误
- **WHEN** 客户端调用 `add`，参数为 `{"a": "hello", "b": 3}`
- **THEN** 响应 MUST 标记为错误，文本内容 MUST 指出参数 `a` 不是合法的数值

#### Scenario: 空参数对象
- **WHEN** 客户端调用 `add`，参数为 `{}`
- **THEN** 响应 MUST 标记为错误，且服务进程 MUST 继续运行以响应后续请求
