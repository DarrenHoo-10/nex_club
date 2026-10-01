# Nex Club

AI 工具网站 · AI焚决集合 · 有价值的 AI GitHub 项目 的前端原型（Vite + React，中文界面）。

```bash
npm install
npm run dev     # http://localhost:5173
npm run build   # 产物在 dist/
```

内容全部来自 `src/data/` 下的 JSON：

- `tools.json`：AI 工具网站
- `tutorials.json`：AI 焚决（教程，含步骤与提示）
- `repos.json`：GitHub 项目

新增条目只需往对应 JSON 数组里追加一项，页面会自动出现，并参与搜索与标签筛选。

## 封面（多张）

每个条目的封面是一个可切换的图集。默认自动生成 3 张示意封面；想用真实截图，在条目里加 `covers`：

```json
{ "id": "claude", "name": "Claude", "covers": ["/covers/claude-1.png", "/covers/claude-2.png"] }
```

图片放在 `public/covers/` 下即可，张数不限。也可用 `coverCount` 调整生成封面的数量。
