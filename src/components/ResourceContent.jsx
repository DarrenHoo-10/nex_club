import { safeHttpUrl, toReaderModel } from '../api/view.js'
import MarkdownBody from './MarkdownBody.jsx'
import Reader from './Reader.jsx'

const URL_LABELS = { tool: '网站网址', tutorial: '参考网址', repo: '项目网址' }

// Shared by the published detail window and the administrator's draft preview.
export default function ResourceContent({ resource, onOutbound }) {
  const href = safeHttpUrl(resource.card?.href)
    || safeHttpUrl(resource.details?.website_url)
    || safeHttpUrl(resource.details?.source_url)

  return <>
    <div className="resource-modal-meta tags">
      {resource.card?.meta && <span className="pill">{resource.card.meta}</span>}
      {(resource.tags || []).filter((tag) => tag.name !== resource.card?.meta).map((tag) => <span className="pill" key={tag.slug}>{tag.name}</span>)}
    </div>
    <p className="resource-modal-summary">{resource.summary}</p>
    {href && <div className="resource-modal-url">
      <span>{URL_LABELS[resource.kind]}</span>
      <a href={href} target="_blank" rel="noopener noreferrer" onClick={onOutbound}>{href}<span aria-hidden="true">↗</span></a>
    </div>}
    {resource.recommendation_reason && <aside className="resource-modal-reason">
      <h3>推荐理由</h3><p>{resource.recommendation_reason}</p>
    </aside>}
    {resource.kind === 'tutorial'
      ? <Reader item={toReaderModel(resource)} showTitle={false} />
      : resource.body_markdown && <MarkdownBody source={resource.body_markdown} />}
  </>
}
