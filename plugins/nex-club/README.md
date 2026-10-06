# Nex Club 内容管理插件

一个 Codex 插件，包含 Nex Club MCP 连接、已有 `manage-content` 内容管理 Skill 和 [`import-tutorial`](skills/import-tutorial/SKILL.md) 教程录入 Skill。本次仅迁移 MCP 接入，不修改或同步 Skill。

## 通过 Nex MCP 网关连接

1. 使用运维提供的 **Nex MCP 网关令牌**，放入启动 Codex 的环境变量 `NEX_MCP_TOKEN`，确保 Codex 进程继承它。不要把令牌写进插件文件、Git 或聊天。
2. `.mcp.json` 连接 `https://mcp.nexorai.com.cn/mcp`。Club 原来的 `/mcp` 返回 410，旧 `NEX_CLUB_MCP_TOKEN` 不再使用。不要把网关令牌发给 Club 公网地址。
3. 确认运维已完成 [只读上游接入](gateway/README.md)。仓库里有配置示例不代表生产已部署或登记。
4. 在仓库根目录把本地市场加入 Codex，再安装：
   ```sh
   codex plugin marketplace add .
   codex plugin add nex-club@nex-club-local
   ```

安装后新建对话加载插件。已有网关连接的客户端可直接使用其 Nex Club 上游，避免重复配置。网关工具带命名空间，请从实际目录发现，不照搬旧直连工具名。客户端默认对所有网关工具询问授权，不对网关其他项目批量自动放行。

## 范围与使用

首次只开放查询；**只读仍能读取草稿和隐藏内容，不等于公开文章**，只有获准访问这些内容的网关用户才能使用。

- “用 Nex Club 上游看看有哪些草稿，先不修改。”
- “这份素材值得收录吗？先和我讨论文章角度。”
- “看看哪些加工任务失败了，告诉我原因。”

服务的 `mcp_list` 提供按权限生成的参数、返回字段、示例和副作用。只读规范名为 `resources_list`、`resource_get`、`tags_list`、`automation_status`、`material_read`、`url_read`、`action_get`；内部保留旧名兼容，但网关白名单仅开放规范名。

网关通道不能准备、保存或执行内容操作，即使客户端加载了已有写入 Skill 也不能绕过服务端限制。编辑、发布和既有操作确认继续在正常登录的 Club 后台进行。写入仍需先展示预览并得到用户明确授权；不会因为网关令牌有效就自动获得写权限。

`read_url` 的纯文本摘录不足以证明完整排版；文档解析与图片上传也不是此 MCP 的能力。现有 Skill 的描述不代表这些能力已由本次只读网关提供。

本插件采用 Codex 兼容格式 `.codex-plugin/plugin.json` + `.mcp.json`，不是已提交到公共插件商店的版本。参见 [插件打包文档](https://developers.openai.com/plugins/build/plugins) 和 [插件 MCP 鉴权](https://developers.openai.com/api/docs/guides/agents-api/tools/plugins)。
