# 信源扩展验收（2026-10-02）

## 实际环境与结果

- 使用本地真实 Go API、worker 与 PostgreSQL，浏览器操作后台。
- 导入 AIHOT 固定版本的 18 条示范 RSS 信源，全部暂停；重复导入新增 0 条、跳过 18 条。
- 逐一试抓 18 个真实 RSS，本轮均返回成功。试抓前后 raw_items、raw_item_revisions、source_runs、processing_runs、resources、provider_calls 的数量完全一致。
- 浏览器试抓 Hugging Face RSS：872 条，展示前 20 条；JSON 试抓 Ollama Releases：5 条；网页 CSS 试抓 Go Blog：278 条。
- 创建仅存档的 JSON 验收信源，首次上限 2 条；首次采集新增 2 条并略过其余 3 条，再次采集未重复入库，仍保持 2 条。验收信源已暂停。
- 修正仅有日期的原文显示：保留原文日历日期，不按浏览器时区移到前一天。
- X / SocialData、公众号 / 极致了适配器使用本地模拟服务验证请求格式、解析、密钥过滤和付费回执；本机未配置这些供应商的密钥与预算，因此没有进行真实付费联调。
- PostgreSQL 覆盖付费试抓的原子预算预占、成功重放、并发同键只发送一次、结果不明阻止新请求、配置变化拒绝复用同键。试抓不会伪造资料或采集任务以满足回执外键。
- 52 项前端测试通过；全量 Go 测试连接独立 PostgreSQL 测试库串行执行。

## 本轮 RSS 检查

| 信源 | 状态 | 解析条数 |
|---|---|---|
| OpenAI News | HTTP 200 | 1243 |
| Google DeepMind | HTTP 200 | 100 |
| Google Research | HTTP 200 | 100 |
| Hugging Face Blog | HTTP 200 | 872 |
| Microsoft Research | HTTP 200 | 10 |
| NVIDIA Blog | HTTP 200 | 18 |
| AWS Machine Learning Blog | HTTP 200 | 20 |
| GitHub Blog · AI & ML | HTTP 200 | 10 |
| Mistral AI | HTTP 200 | 88 |
| Berkeley AI Research | HTTP 200 | 10 |
| The Verge · AI | HTTP 200 | 10 |
| TechCrunch · AI | HTTP 200 | 19 |
| Ars Technica · AI | HTTP 200 | 20 |
| MIT Technology Review · AI | HTTP 200 | 10 |
| The Decoder | HTTP 200 | 10 |
| Simon Willison | HTTP 200 | 30 |
| Import AI | HTTP 200 | 20 |
| Latent Space | HTTP 200 | 20 |

本轮可访问不代表供应商永久可用；未来拒绝访问、限流或格式变化会显示在试抓与采集任务中。

技术与配置说明见 [多渠道采集设计](designs/11-source-collectors.md)。
