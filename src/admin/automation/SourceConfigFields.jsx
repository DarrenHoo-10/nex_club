export function sourceConfig(kind) {
  const common = { initial_backfill_limit: 8 }
  if (kind === 'json') return { ...common, url: '', itemsPath: '', titlePaths: ['title', 'name'], urlPaths: ['url', 'html_url'], summaryPaths: [], bodyPaths: [], authorPaths: [], publishedAtPath: '', publishedAtUnit: 'auto', externalIdPath: '', urlTemplate: '' }
  if (kind === 'web') return { ...common, url: '', itemSelector: '', linkSelector: 'a', titleSelector: 'a', summarySelector: '', publishedAtSelector: '', publishedAtUtcOffset: '' }
  if (kind === 'x') return { ...common, query: '', searchType: 'Latest' }
  if (kind === 'wechat') return { ...common, ghid: '', nickname: '' }
  return common
}
export default function SourceConfigFields({ kind, config, onChange }) {
  const text = (label, key, placeholder = '', wide = false) => <label className={`field${wide ? ' auto-wide' : ''}`} key={key}>{label}<input value={config[key] || ''} placeholder={placeholder} onChange={(e) => onChange({ [key]: e.target.value })} /></label>
  const paths = (label, key, placeholder = '') => <label className="field" key={key}>{label}<input value={(config[key] || []).join(', ')} placeholder={placeholder} onChange={(e) => onChange({ [key]: e.target.value.split(',').map((p) => p.trim()) })} /></label>
  return <>
    {kind === 'json' && <>
      {text('接口网址（GET）', 'url', 'https://example.com/api/articles', true)}
      {text('列表所在字段', 'itemsPath', '例如 data.items；根节点就是数组时留空')}
      {paths('标题字段', 'titlePaths', '例如 title, name')}
      {paths('原文链接字段', 'urlPaths', '例如 html_url, url')}
      {text('原文链接模板（可选）', 'urlTemplate', 'https://example.com/posts/{id}')}
      {paths('摘要字段', 'summaryPaths', '例如 summary, description')}
      {paths('正文字段（可选）', 'bodyPaths', '例如 body, content')}
      {paths('作者字段（可选）', 'authorPaths', '例如 author.name')}
      {text('条目编号字段（可选）', 'externalIdPath', '例如 id')}
      {text('发布时间字段（可选）', 'publishedAtPath', '例如 published_at')}
      <label className="field">日期格式<select value={config.publishedAtUnit || 'auto'} onChange={(e) => onChange({ publishedAtUnit: e.target.value })}><option value="auto">自动识别 ISO 日期 / 时间戳</option><option value="seconds">Unix 秒</option><option value="milliseconds">Unix 毫秒</option><option value="yyyymmdd">YYYYMMDD</option></select></label>
      <p className="hint auto-wide">字段可以用点号表示层级，例如 data.title；多个候选字段用逗号分隔。只读取公开 GET 接口，不执行脚本。</p>
    </>}
    {kind === 'web' && <>
      {text('网页列表网址', 'url', 'https://example.com/news', true)}
      {text('每条内容的选择器', 'itemSelector', '例如 .news-list > article')}
      {text('链接选择器', 'linkSelector', '例如 h2 a')}
      {text('标题选择器', 'titleSelector', '例如 h2 a')}
      {text('摘要选择器（可选）', 'summarySelector', '例如 .summary')}
      {text('日期选择器（可选）', 'publishedAtSelector', '例如 time')}
      {text('日期时区（可选）', 'publishedAtUtcOffset', '例如 +08:00')}
      <p className="hint auto-wide">选择器应匹配每条文章，不是整个列表容器。普通 HTML 模式不执行 JavaScript；动态页面可改用 RSS、JSON 或外部推送。</p>
    </>}
    {kind === 'x' && <>
      {text('X 搜索条件', 'query', 'from:OpenAI -filter:replies', true)}
      <label className="field">搜索排序<select value={config.searchType || 'Latest'} onChange={(e) => onChange({ searchType: e.target.value })}><option value="Latest">最新</option><option value="Top">热门</option></select></label>
      <p className="hint">通过 SocialData 获取当前一页结果。试抓同样会计入服务商费用与预算。</p>
    </>}
    {kind === 'wechat' && <>
      {text('公众号原始 ID', 'ghid', 'gh_xxxxxxxx')}
      {text('公众号昵称（可选）', 'nickname', '用于标注来源')}
      <p className="hint auto-wide">通过极致了（Dajiala）采集。试抓只查文章列表；正式采集新文章时，还会按预算获取正文。</p>
    </>}
  </>
}
