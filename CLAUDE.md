## SDD 工作流（Spec-Driven Development）

本项目采用 SDD 方法论进行 AI 辅助开发，使用 OpenSpec v2.x 作为规范框架。

### 规范目录
- `openspec/specs/` —— 权威基准（系统当前行为的规范）
- `openspec/changes/` —— 进行中的变更（每个变更一个文件夹）
- `openspec/changes/archive/` —— 已归档的变更

### 工作纪律
- 任何新功能/重构/修复，必须先通过 /opsx:propose 创建变更再写代码
- 实现过程中发现产物有误，先修改产物文件再修改代码
- 每个 Task 独立一次 commit，commit message 引用变更名和任务编号
- 上下文超过 40% 时，主动清理并从变更产物恢复

### 标准四产物
1. `proposal.md` —— 提案（人类主导，定义意图和范围）
2. `specs/<domain>/spec.md` —— 行为规范（AI 生成，人类审批）
3. `design.md` —— 技术方案（AI 生成，人类审批）
4. `tasks.md` —— 实现清单（AI 生成，人类审批）

### Skills 与 OpenSpec 协作
- 实现 Task 时，先读取对应的 spec 和 design，再按 Skill 规范写代码
- Skill 规定"代码怎么写"，spec 规定"代码做什么"，两者不矛盾时以 spec 为准
- 新建 Skill 时，应参考 openspec/specs/ 中的现有领域划分