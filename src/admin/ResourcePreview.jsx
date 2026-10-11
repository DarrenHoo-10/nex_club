import { useState } from 'react'
import Card from '../components/Card.jsx'
import ResourceContent from '../components/ResourceContent.jsx'
import ResourceDialog from '../components/ResourceDialog.jsx'
import { toCardModel, toGalleryKind } from '../api/view.js'

export default function ResourcePreview({ resource, banner, onClose, onPublish, canPublish = false }) {
  const [view, setView] = useState('detail')
  const [confirmPublish, setConfirmPublish] = useState(false)
  const model = toCardModel(resource)

  return <ResourceDialog
    title={resource.title}
    eyebrow="发布前预览"
    closeLabel="返回编辑"
    onClose={onClose}
    reading={resource.kind === 'tutorial' && view === 'detail'}
    contentVersion={resource}
    actions={onPublish ? (
      <button
        type="button"
        className="pill on resource-modal-publish"
        disabled={!canPublish}
        title={canPublish ? '发布当前已保存的草稿' : '请先保存草稿'}
        onClick={() => { if (canPublish) setConfirmPublish(true) }}
      >发布</button>
    ) : null}
    toolbar={<>
      <div className="preview-toolbar">
        <p className="preview-banner" role="status">{banner}</p>
        <div className="sort-control pill-glass" role="group" aria-label="预览方式">
          <button type="button" className={`sort-button${view === 'detail' ? ' on' : ''}`} aria-pressed={view === 'detail'} onClick={() => setView('detail')}>详情效果</button>
          <button type="button" className={`sort-button${view === 'card' ? ' on' : ''}`} aria-pressed={view === 'card'} onClick={() => setView('card')}>卡片效果</button>
        </div>
      </div>
      {confirmPublish && (
        <div className="preview-publish-confirm glass strong" role="dialog" aria-labelledby="preview-publish-title">
          <h3 id="preview-publish-title">确认发布</h3>
          <p>公开页将显示当前已保存的草稿。</p>
          <div className="confirm-actions">
            <button type="button" className="pill" onClick={() => setConfirmPublish(false)}>取消</button>
            <button type="button" className="btn primary" onClick={() => { setConfirmPublish(false); onPublish() }}>确认发布</button>
          </div>
        </div>
      )}
    </>}
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
