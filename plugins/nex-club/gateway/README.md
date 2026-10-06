# MCP 接入 Nex MCP 网关

本次仅处理 MCP，不同步或修改 Skill。**本文件是部署步骤，不表示生产已部署或上游已登记。**

## 认证与信任边界

```text
客户端 ── 网关 Bearer 认证 ── nex_mcp（nex-mcp 系统账号）
                                │ stdio：api mcp-bridge
                                ▼
                  /run/nex-club-mcp/mcp.sock（0660）
                                │ Unix socket，仅本机文件权限授权
                                ▼
                     Club API 内的只读 MCP 服务
```

- 客户端只持有网关令牌（本插件读取 `NEX_MCP_TOKEN`），不再创建或配置 `NEX_CLUB_MCP_TOKEN`。
- Club 公网 `/mcp` 及其子路径固定返回 **410 Gone**，无论旧令牌、网关令牌、Cookie、Origin、loopback 来源或伪造转发/身份头。内部通道没有 HTTP 入口，不依赖可信请求头，也不新增 TCP 端口。
- 默认不启动内部 MCP。必须同时配置 `NEX_MCP_SOCKET` 绝对路径和非零 `NEX_MCP_ADMIN_ID`；Linux 以外不启用此监听。目录须预先创建、归 API 用户所有、无 group-write/other 权限，不允许目录本身为符号链接。socket 权限固定 0660；路径已存在时拒绝启动，不会自动删除已有文件或活跃 socket。
- 服务身份是运维指定的 **专用、active 的 Club 管理员 UUID**，不是令牌或客户端传入的身份。每次工具调用重新检查状态，停用该管理员立即阻断后续调用；数据库不可用时拒绝访问。所有网关用户共享此身份的读取范围，`action_get` 仅能读取它拥有的操作，不会冒充客户端原来的个人管理员。
- 这是管理端只读：包含草稿、隐藏内容、素材和自动化状态。**网关当前使用者必须全部有权读取这些内容**；不能在向公众开放的网关上直接启用该上游。socket 组成员、宿主 root 和可控制 Club 容器的运维也处于信任边界内。没有逐客户端身份传递。
- 浏览器后台登录、原有操作预览/确认不变。历史 Club 令牌保留列表与撤销能力，但不再被 MCP 接受，也不能创建新令牌；本变更没有删除数据库表或数据。

## 目录与兼容性

项目 ID `nex-club`，上游 ID `content`。只暴露 `upstream.readonly.json` 中 8 个入口：`mcp_list` + 7 个查询工具。新旧写入名均不会在内部服务注册，修改网关白名单也无法开启写入。

| 原名 | 规范名 |
| --- | --- |
| `list_resources` | `resources_list` |
| `get_resource` | `resource_get` |
| `list_tags` | `tags_list` |
| `automation_status` | `automation_status` |
| `read_material` | `material_read` |
| `read_url` | `url_read` |
| `get_action` | `action_get` |

旧工具名在服务内部兼容，但旧公网客户端必须迁移到网关。网关仅开放规范名，完整名字由其命名空间生成，**从实际 tools/list 或 projects_list.mcpListCallName 读取，不手写哈希**。

`mcp_list({tool})` 返回当前身份实际可见接口的参数、示例、输出字段和副作用；省略参数返回全部。接口版本 `2.0.0` 标记直连认证的破坏性变更。`material_read` 返回 `{items:[...]}`；兼容名 `read_material` 的文本保持数组、structuredContent 为对象。其他既有文本结果不变，新增 outputSchema 与 structuredContent。错误增加 `error`，保留内部兼容的 code/message/status。

## 部署准备（单独运维授权后执行）

已核实的拓扑：Club 在首尔 Compose `nex-club` 中运行，API 用户 10001:10001；网关是同机 systemd `nex-mcp.service`，用户 `nex-mcp`。**不要把该用户加入 docker 组，不要向其提供 Club 数据库/会话密钥，不要改防火墙或重启 nginx。**

1. 按 `deploy/README.md` 构建新 API 和发布包。相同 Linux `api` 可执行文件复制到宿主 `/opt/nex-club-mcp/api`，目录及文件由 root 管理、网关不可写，文件 0755。`api mcp-bridge <socket>` 在加载 API 配置前进入转发模式，只需要 socket 权限；stdout 只有协议字节，诊断走 stderr。
2. 选择专用 active 管理员 UUID，配置到部署 `.env` 的 `NEX_MCP_ADMIN_ID`（它不是秘密）。不要复用个人管理员身份来共享个人待确认操作。管理员创建沿用正式管理流程，不自动选第一位管理员。
3. 检查宿主 GID **21001** 未被其他业务使用，为它创建专用组 `nex-club-mcp`；如冲突，选择空闲 GID 并同步修改下面配置与 `compose.mcp.yaml`。只让 Club API 与网关访问，不能加入普通用户或其他服务。
4. 运维创建 tmpfiles 规则 `/etc/tmpfiles.d/nex-club-mcp.conf`，并通过 `systemd-tmpfiles --create` 创建目录：
   ```text
   d /run/nex-club-mcp 2750 10001 nex-club-mcp -
   ```
   setgid 保证新 socket 继承专用组；父目录不可由非受信任用户替换。宿主重启会清空 `/run`，tmpfiles 必须先于 Club 启动完成。不要使用世界可读写目录或 chmod 777。
5. 为 `nex-mcp.service` 添加经过备份和审查的 drop-in：
   ```ini
   [Service]
   SupplementaryGroups=nex-club-mcp
   ```
   同时核对现有 systemd sandbox 允许 AF_UNIX、访问该路径和执行只读的 bridge 二进制，不放宽其他限制。生效需要单独获准的 daemon-reload / 网关服务重启。操作前备份配置为 `.bak-<时间戳>`。
6. 使用仓库 `deploy/compose.mcp.yaml` 作为**可选 overlay**，例如 `docker compose -f compose.yaml -f compose.mcp.yaml up -d --wait api`。常规 Compose 文件不自动暴露 MCP，也没有新增端口。后续更新必须继续携带 overlay。目录缺失、权限错误或服务身份无效会使 API 启动失败，不能为恢复可用性改成匿名 HTTP。
7. 若上次进程被强杀留下 socket，先停下本项目 API、确认没有活跃监听者，再由运维移除**这一条经过确认的 socket 文件**。应用不会无条件 unlink。正常退出会关闭会话并清理 socket。

## 网关配置与登记

网关现有 HTTP 上游规则 R10 要求 bearerTokenEnv。本方案使用其**已经支持的 stdio 上游**，不伪造 token、不关闭规则、不需要修改 `nex_mcp` 代码。

`upstream.readonly.json` 是 `projects[].mcpServers[]` 的单个配置对象，**不是 mcp_upstream_create 的 HTTP payload**，没有 projectId/dryRun 字段。stdio 命令只能由运维配置文件管理，不能通过 MCP API 注册任意进程。

1. 先查询 `project_get` 与 `mcp_upstream_list({projectId:"nex-club"})`。项目已存在时不重建；如已有同 ID HTTP 上游，先停用并制定显式迁移，不覆盖或删除后重建。
2. 备份网关实际使用的配置文件，仅在 `nex-club` 项目配置条目 `mcpServers` 中增量加入此对象，保留其他项目、服务器和上游。网关会同步配置中的 stdio 条目到现有项目，不会以整个文件覆盖项目数据库。
3. 运行网关本身 `validate --config <实际配置>`，再在运维批准的窗口重启**网关本服务**加载 stdio 配置；这里没有 HTTP dryRun 热登记。不要把配置已写入说成上游已可用。
4. 用网关查询上游状态、工具目录与合规结果。所有自动检查（R1–R5、R8、R10）应通过，暴露入口仅 8 个。网关可保存离线文档快照，验收还需要实际查询成功，不能只看目录。

## 验收与回退

- 公网 Club `/mcp`：无凭据、旧 Club 令牌、网关令牌、伪造转发头均 410；浏览器登录与管理流程正常。
- 网关：无效/缺失网关凭据不可查询；有效且获准的网关凭据完成 initialize、tools/list、mcp_list 全量/筛选及 7 个查询能力。
- 非 socket 组的本机普通账号无法连接；网关账号可连接；文件权限为目录 2750 / socket 0660，所有者与组正确。
- 新旧 prepare/execute 工具均无法列出或调用，mcp_list 不泄露其文档；url_read 拒绝私网请求。停用服务管理员后，既有会话下一次调用也失败。
- 没有该服务身份的 action ID 时，将 action_get 标记为未验证，不为验收创建写操作；不要把草稿全文输出到日志。
- 回退优先禁用此次 `content` 上游，停止内部 socket/移除 overlay，保留公网关闭。恢复旧 API 镜像会重新开启旧 bearer 直连，因此必须先处理历史令牌或在边界明确阻断 `/mcp`，不能直接宣称安全等价。不删除数据库卷、历史操作或其他项目。

## 本地验证

```sh
go -C server test ./internal/httpapi/admin -run 'TestMCP(Catalog|Legacy|Gateway|Private)' -count=1
go -C server test ./internal/mcptransport ./internal/platform/config ./internal/httpapi/static
npm test -- --maxWorkers=1 --minWorkers=1
npm run build
```

权限/socket 全链路测试在 Linux 上运行。数据库测试需要仅指向本机测试库的 `NEX_TEST_DATABASE_URL`；未配置会跳过，不能当作通过。完整后端验收另运行 `go -C server test ./...`。

可设置 `NEX_MCP_CONTRACT_FILE`，运行 `TestMCPExportGatewayContract` 导出只读目录，交给网关真实 `runCompliance` 做离线检查；该文件不包含生产数据或凭据。离线合规不等同生产连通性验收。
