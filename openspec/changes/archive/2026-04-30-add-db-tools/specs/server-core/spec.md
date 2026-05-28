## MODIFIED Requirements

### Requirement: 入口瘦身
`main.go` SHALL 仅承担以下职责：(1) 解析命令行 flag（例如 `--config`）；(2) 调用配置加载函数读取 YAML；(3) 构造 MCP server 实例；(4) 向服务注册所有工具（注册函数可接收配置对象作为参数）；(5) 启动 stdio 传输。`main.go` MUST NOT 包含任何具体工具的参数 schema 定义、具体工具 `arguments` 解析逻辑、业务计算逻辑、SQL 构造或直接的数据库访问。

#### Scenario: main.go 不包含工具实现细节
- **WHEN** 审阅 `main.go` 的源码
- **THEN** `main.go` MUST NOT 出现任何工具参数（如 `a`、`b`、`sql`、`source`）的解析或业务处理代码
- **AND** 每个具体工具的注册 MUST 通过调用该工具所在子包导出的 `Register` 函数完成
- **AND** `main.go` MAY 直接调用 `flag` 包与项目内部配置加载函数

#### Scenario: main.go 可以加载配置并传递
- **WHEN** 审阅 `main.go` 的源码
- **THEN** `main.go` MAY 解析 `--config` flag、调用项目内 `config.Load(path)`、将返回的 `*config.Config` 作为参数传给某些工具的 `Register` 函数
- **AND** 对配置字段的业务解释（例如根据 `mode` 决定是否允许某类 SQL）MUST 由工具子包自身负责，MUST NOT 出现在 `main.go` 中
