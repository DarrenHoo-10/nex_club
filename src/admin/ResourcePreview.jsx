import { useState } from 'react'
import Card from '../components/Card.jsx'
import ResourceContent from '../components/ResourceContent.jsx'
import ResourceDialog from '../components/ResourceDialog.jsx'
import { toCardModel, toGalleryKind } from '../api/view.js'

export default function ResourcePreview({ resource, banner, onClose }) {
  const [view, setView] = useState('detail')
  const model = toCardModel(resource)

  return <ResourceDialog
    title={resource.title}
    eyebrow="发布前预览"
    closeLabel="返回编辑"
    onClose={onClose}
    reading={resource.kind === 'tutorial' && view === 'detail'}
    contentVersion={resource}
    toolbar={<div className="preview-toolbar">
      <p className="preview-banner" role="status">{banner}</p>
      <div className="sort-control pill-glass" role="group" aria-label="预览方式">
        <button type="button" className={`sort-button${view === 'detail' ? ' on' : ''}`} aria-pressed={view === 'detail'} onClick={() => setView('detail')}>详情效果</button>
        <button type="button" className={`sort-button${view === 'card' ? ' on' : ''}`} aria-pressed={view === 'card'} onClick={() => setView('card')}>卡片效果</button>
      </div>
    </div>}
  >
    {view === 'detail' ? <ResourceContent resource={resource} /> : <div className="preview-card">
      <Card
        item={model}
        kind={toGalleryKind(resource.kind)}
        title={model.title}
        sub={model.subtitle}
        desc={model.desc}
        meta={model.meta}
        onClick={() => setView('detail')}
      />
      <p className="hint">点击卡片可查看详情效果</p>
    </div>}
  </ResourceDialog>
}
