
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
    page <-->|"对话 · 进度与结果"| lingxi
    tools -->|"注册能力"| lingxi
    lingxi -->|"调用已注册的工具"| tools
    tools -->|"调用端侧 Agent"| agents
    agents -.->|"执行状态 · 代码变更 · 测试结果"| tools
    screen -->|"提供画面来源"| casting
    casting -->|"发起投屏 · AirPlay 等协议"| display
    casting -->|"网页画面流"| page
```

MCP Server 一方面向灵矽注册编程工具能力，供灵矽在任务编排时调用；另一方面通过 Web Server 提供页面，供手机 / Pad 浏览器访问。用户在页面上与灵矽对话，灵矽调用 MCP Server 的能力驱动端侧 Codex / Claude 在本地仓库中读取、编写和测试代码，进度与结果再经灵矽返回页面。

实时画面是独立于任务结果的链路：MCP Server 获取当前工作画面，并负责发现接收端、发起和控制向外投屏（如使用 AirPlay 等兼容协议）。手机 / Pad 也可在 MCP Server 提供的网页中观看画面，并继续通过页面与灵矽交互；网页观看不等于 AirPlay 接收。画面获取和投屏应仅在用户授权后开启，并限制可观看的设备与会话。
