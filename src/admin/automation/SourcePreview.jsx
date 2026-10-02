import ResourceDialog from '../../components/ResourceDialog.jsx'
import { safeHttpUrl } from '../../api/view.js'
import { date } from './common.jsx'

export default function SourcePreview({ result, name, onClose }) {
  return <ResourceDialog title="试抓结果" eyebrow={name || '信源预览'} closeLabel="返回配置" onClose={onClose}>
    <div className="source-preview-summary"><strong>{result.total} 条</strong><span>{(result.elapsed_ms / 1000).toFixed(1)} 秒 · 展示前 {result.items.length} 条</span></div>
    <p className="auto-notice">仅展示抓取结果，没有写入资料库，也没有启动 AI 加工。{result.paid ? '付费请求已计入调用回执与预算。' : ''}</p>
    {result.warnings?.map((warning) => <p className="hint" key={warning}>{warning}</p>)}
    {result.items.length === 0 && <p className="empty">来源暂时没有可展示的条目。</p>}
    <ol className="source-preview-list">{result.items.map((item, index) => <li key={`${item.url}-${index}`}>
      <h3>{safeHttpUrl(item.url) ? <a href={safeHttpUrl(item.url)} target="_blank" rel="noopener noreferrer">{item.title} <span aria-hidden>↗</span></a> : item.title}</h3>
      <small>{item.published_date ? `原文日期：${item.published_date}` : item.published_at ? date(item.published_at) : '原文日期未知'}{item.author ? ` · ${item.author}` : ''}</small>
      {item.summary && <p>{item.summary}</p>}
      <span className="source-preview-url">{item.url || '未提供原文链接'}</span>
    </li>)}</ol>
  </ResourceDialog>
}
