
```mermaid
flowchart LR
    subgraph client["用户入口"]
        device["手机 / Pad 浏览器"]
        display["电视等投屏接收端"]
    end

    subgraph server["MCP Server"]
        page["Web 页面<br/>由 Web Server 提供"]
        tools["MCP 能力<br/>任务下发 · 结果回传"]
        casting["画面采集与投屏<br/>获取画面 · 发起 / 控制投屏"]
    end
    lingxi["灵矽<br/>接收编程任务 · 理解需求"]

    subgraph edge["端侧开发环境"]
        agents["Codex / Claude<br/>执行编程任务"]
        workspace["本地代码仓库<br/>读取代码 · 修改文件 · 运行测试"]
        screen["当前工作画面"]
        agents <--> workspace
    end

    device <-->|"访问网页 · 提交需求 · 查看进度"| page
    page <-->|"语音对话 · 回复文字与音频"| lingxi
    tools -->|"Codex / Git 事件 · 独立执行窗口"| page
    tools -->|"注册能力"| lingxi
    lingxi -->|"调用已注册的工具"| tools
    tools -->|"调用端侧 Agent"| agents
    agents -.->|"执行状态 · 代码变更 · 测试结果"| tools
    screen -->|"提供画面来源"| casting
    casting -->|"发起投屏 · AirPlay 等协议"| display
    casting -->|"网页画面流"| page
```

MCP Server 一方面向灵矽注册编程工具能力，供灵矽在任务编排时调用；另一方面通过 Web Server 提供页面，供手机 / Pad 浏览器访问。灵矽对话区仅展示 `tts / sentence_start` 返回的语音对应文字并外放二进制音频，不展示用户 STT、Codex 或 Git 消息。Codex 实际输出的执行消息、Git 阶段、diff 和 PR 直接由 MCP Server 推送到浏览器，独立执行窗口通过同源 opener 消息接收历史及实时事件，不建立重复音频连接。该窗口依赖主窗口存活，不持久化历史；浏览器不支持 WebCodecs 时以语音合成朗读同一回复文字。

## 可运行 MVP

Go MCP Server 同时承担三条链路：

1. 作为灵矽设备端，使用 YAML 中的 WSS 地址和 `Authorization`、`Device-Id`、`Client-Id`、`Protocol-Version` 请求头主动连接，发送 `hello` 并声明 MCP 能力；连接建立后等待并校验服务端 hello，断线按配置重连。
2. 处理灵矽封装在 `type: mcp` 中的 JSON-RPC `initialize`、`tools/list` 和 `tools/call`。当前注册的工具是 `self.code.request`。
3. 通过 HTTP 托管 `frontend/index.html`，通过 `/ws/ui` 双向传输任务状态、Codex CLI `--json` 逐行事件、工作区 diff、PR 信息和二进制音频。浏览器上传的录音转发给灵矽，灵矽返回的音频经同一 WebSocket 广播给浏览器播放。

`tools/call` 创建非空任务后，Codex 适配器以 `--sandbox workspace-write` 在配置的 Git 仓库工作区运行，服务端使用 `git add -N -- .` 将新文件标记为 intent-to-add 后读取 `git diff`，创建 `lingxi/<task-id>` 分支并提交，再使用 GitHub API 创建 Pull Request。工作区必须是实际项目仓库，不能填写仓库父目录；服务启动时会提前校验。空任务不会进入 Codex 或 `git diff`。浏览器的 Merge 按钮打开 PR 新标签页，不绕过 GitHub 审查直接合并。

实时画面是独立于任务结果的目标链路：MCP Server 应获取当前工作画面，并负责发现接收端、发起和控制向外投屏（如使用 AirPlay 等兼容协议）。当前代码尚未实现真实画面采集、接收端发现、AirPlay 投屏或网页画面流；手机 / Pad 网页目前只展示任务状态和 diff。实现时必须在用户授权后开启，并限制可观看的设备与会话。
