# Nex Club 内容管理插件

一个 Codex 插件，包含 Nex Club MCP 连接和 `manage-content` Skill。讨论与写作发生在现有 AI 对话中；Nex Club 服务端负责资源与采集命令，不运行一套独立聊天模型。

## 连接本地项目

1. 启动 Nex Club API，默认开发入口为 `http://localhost:8089`。
2. 在后台「自动化 → MCP 接入」创建令牌。默认只读；需要 AI 准备操作时打开准备权限，需要在 AI 客户端执行时再打开执行权限。
3. 把令牌放入启动 Codex 的环境变量 `NEX_CLUB_MCP_TOKEN`，确保 Codex 进程继承该变量。不要写进插件文件、Git 或聊天内容。
4. 在仓库根目录把本地市场加入 Codex，再安装：

```sh
codex plugin marketplace add .
codex plugin add nex-club@nex-club-local
```

也可以在应用的插件目录中找到本项目提供的 Nex Club。安装后新建对话加载插件。首次可以要求：“用 Nex Club 插件看看有哪些草稿，先不修改。”

`.mcp.json` 默认连接本地地址。连接已部署的服务时，把 `url` 改为该实例的 HTTPS `/mcp` 地址，再打包/安装；不要把生产令牌放入配置。

## 使用

- “这份素材值得收录吗？先和我讨论文章角度。”
- “把我们讨论的内容整理成教程草稿，展示预览。”
- “这份预览确认无误，发布这一版。”
- “看看哪些加工任务失败了，告诉我原因。”

读工具默认允许；准备、网页读取和执行工具默认由客户端询问授权。执行还需要服务端令牌权限与匹配的预览摘要。只允许准备的连接可以在后台完成最终确认。

本版使用个人 Bearer 令牌和 Streamable HTTP，适用于本地 Codex / 支持此鉴权方式的客户端。没有 OAuth 登录；不是已经提交到公共插件商店的版本。令牌 30 天过期，随时可在后台撤销。

插件采用官方仍支持的 Codex 兼容格式 `.codex-plugin/plugin.json` + `.mcp.json`，与当前本地安装器一致。参见 [插件打包文档](https://developers.openai.com/plugins/build/plugins) 和 [插件 MCP 鉴权](https://developers.openai.com/api/docs/guides/agents-api/tools/plugins)。
