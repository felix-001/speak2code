# AGENTS.md

## 项目概览

`speak2code` 是一个探索“用自然语言驱动本地编码 Agent”的概念验证项目。目前包含静态交互原型和架构文档，尚未接入真实服务。

## 架构约束

- **MCP Server** 有两项独立职责：向灵矽注册编程工具能力并接受调用；通过 Web Server 提供手机 / Pad 可访问的网页。
- 手机 / Pad 访问 MCP Server 提供的网页，并在网页中与灵矽交互。网页交互不能被描述成手机直接调用 MCP 工具。
- 灵矽负责理解需求和编排任务，调用 MCP Server 已注册的工具能力。
- MCP Server 负责把任务交给端侧 Codex / Claude，并接收执行状态、代码变更和测试结果。
- **向外投屏本身由 MCP Server 负责**：获取当前工作画面、发现接收端、发起和控制 AirPlay 等兼容协议投屏。端侧开发环境只提供当前工作画面来源，不负责投屏。
- 手机 / Pad 也可以通过 MCP Server 的网页观看实时画面；网页观看不等于 AirPlay 接收。
- 画面获取、网页观看和投屏都需要考虑用户授权、会话隔离和观看权限。

架构发生变化时，必须同步更新 `docs/design/architecture.md` 和 `README.md`，并确保两处 Mermaid 图表达一致。

## 目录与修改范围

- `docs/design/architecture.md`：系统角色、数据流和职责边界。
- `prototypes/index.html`：无需构建即可打开的静态交互原型。
- `README.md`：项目说明、当前能力、边界和演进建议。
- `docs/drafts/`：设计草稿目录，除非任务明确要求，否则不要添加无关内容。

保持修改聚焦，不要为了文档调整引入包管理器、构建工具或后端服务。

## 开发与验证

当前没有包管理器、构建工具或自动化测试配置。

单元测试覆盖率必须达到 90% 以上。

修改静态原型后：

1. 使用 `open prototypes/index.html` 打开页面，检查页面能正常加载。
2. 需要验证交互时，检查演示任务、语音输入、重置和合并确认等现有流程未被破坏。
3. 文档或 Mermaid 图修改后运行：

```bash
git diff --check
test -f prototypes/index.html
test -f docs/design/architecture.md
```

不要把静态演示中的模拟响应描述成真实服务能力。

## 编辑约定

- 默认使用 ASCII；只有在项目已有中文文案或确有必要时使用中文字符。
- 保留现有页面视觉风格和无构建依赖。
- 不修改与任务无关的文件，不覆盖用户已有的未提交修改。
- 提交前检查 `git diff --check` 和 `git status --short`。
