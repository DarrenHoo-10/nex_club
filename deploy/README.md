# 首尔生产部署

入口：`https://club.nexorai.com.cn`，后台 `/admin`。Club 公网 MCP `/mcp` 已停用（410）；MCP 客户端统一通过 Nex MCP 网关认证。

网关内部连接为可选 Linux Unix socket，默认关闭，不新增网络端口，也不再使用 Club MCP 令牌。部署 overlay、专用服务身份、socket 权限与网关 stdio 配置见 [MCP 网关接入说明](../plugins/nex-club/gateway/README.md)。这是需要单独安排的破坏性迁移：只更新 API 会关闭旧直连，不会自动让网关可用。

Web 静态资源由 Go API 同源提供。API、Worker、PostgreSQL 使用独立的 Compose 项目 `nex-club`，API 仅映射 `127.0.0.1:8089`，数据库不开放宿主端口。宿主 Nginx 使用该域名的独立配置与 Let's Encrypt 证书，证书由现有 Certbot 任务续期并平滑加载。

## 目录与配置

服务器工作目录 `~/projects/nex_club`：

- `compose.yaml`：来自本目录的 `compose.production.yaml`。
- `.env`：仅保存 `NEX_IMAGE=nex-club:<发布版本>`。
- `secrets/runtime.env`：运行数据库连接、生产环境配置、会话/游标密钥、CPA 模型设置。
- `secrets/postgres.env`：独立 PostgreSQL 的初始化账号。
- `secrets/migration.env`：仅提供给一次性迁移容器的数据库所有者凭据。
- `secrets/admin.env`：初始化管理员所需的环境变量。
- `releases/<发布版本>`：Linux amd64 可执行文件、dist 和镜像构建文件。
- `backups/`：数据库备份。

secrets 目录权限 700，环境文件权限 600；它们不进入镜像、Git 或公开静态目录。运行角色 `nex_runtime` 仅继承 `nex_app`，没有建表、修改审计记录的权限。API、Worker 使用非 root 用户、只读根文件系统与资源上限。

生产环境配置：`NEX_ENVIRONMENT=production`、`NEX_PUBLIC_INCLUDE_DEMO=false`、`NEX_MODEL_FIXTURE=false`、`NEX_PUBLIC_BASE_URL=https://club.nexorai.com.cn`。模型沿用 CPA 的 Gemini 配置；信源全部暂停时不会自动采集或触发模型加工。

## 构建

可直接从仓库根目录构建 Linux 镜像：

```sh
revision=$(git rev-parse --short HEAD)
docker build --platform linux/amd64 --build-arg VERSION="$revision" -f server/Dockerfile -t "nex-club:$revision" .
```

首尔机器资源有限，实际首次部署采用本机编译：`npm run build`，并分别为 `cmd/api`、`cmd/worker`、`cmd/nexadm` 设置 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 编译。通过 `-ldflags "-s -w -X github.com/darrenhoo/nex_club/server/internal/platform/buildinfo.Version=$revision"` 注入发布版本。

把三个程序放入发布目录的 `bin/`，前端产物放入 `dist/`，将 `Dockerfile.release` 复制为该目录的 `Dockerfile`。上传后在发布目录构建镜像。发布包只包含可执行程序与前端产物。

## 首次初始化

准备好上述环境文件和镜像，在服务器工作目录执行 `python3 bootstrap-production.py`。脚本等待数据库健康、执行应用与 River 迁移、创建受限运行角色、初始化首位管理员，最后启动 API 与 Worker。已有运行密码不重置，已有管理员不重建。脚本接受生成的 48 位十六进制运行数据库密码。

本次按照用户选择采用全新生产库；未导入演示或验收文章。通过后台导入了 18 个 AIHOT RSS 示例定义，全部暂停。启用信源或发布内容由管理员后续操作。

## 升级、备份与回退

更新配置前保存 `<文件>.bak-<UTC 时间戳>`。在工作目录运行 `./backup.sh` 得到 PostgreSQL 自定义格式备份，再保存当前 NEX_IMAGE 值。

上传并构建新镜像后更新 `.env` 的 NEX_IMAGE。先运行 `docker compose run --rm migrate`，成功后运行 `docker compose up -d --wait api worker`。验证 `/health/ready` 的版本和状态，再验证页面、后台及 MCP。

仅在数据库迁移向后兼容时，才可以把 NEX_IMAGE 改回之前版本并重新启动本项目的 API/Worker。涉及不兼容迁移时，先制定数据库恢复步骤。不要通过 `down -v` 回滚，不删除生产卷，不重启其他项目。

Nginx 配置先 `nginx -t`，通过后只使用 reload；不 restart，不修改防火墙或其他站点。首次配置文件备份留在 `/etc/nginx/conf.d/club.nexorai.com.cn.conf.bak-*`，备份后缀不会匹配 `*.conf`。

## 验证

历史首次上线曾验证公网 MCP 初始化/读取/鉴权/撤销（验收令牌已撤销）。该记录不适用于新的网关内部通道；新版本必须另外验证公网 `/mcp` 返回 410、网关认证、socket 文件权限与只读工具隔离，以及原有页面、后台登录与 Secure Cookie。

Cloudflare 会拒绝默认 Python urllib 的客户端签名。部署检查使用明确的应用 User-Agent `NexClub-DeploymentCheck/1.0`；站点和标准 curl 客户端正常。未降低 Cloudflare 或服务器的安全设置。
