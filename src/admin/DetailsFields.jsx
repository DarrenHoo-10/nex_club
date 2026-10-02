import { DEPLOYMENTS, LEVELS, PLATFORMS, PRICING } from './labels.js'

export default function DetailsFields({ form, onDetails }) {
  const details = form.details
  if (form.kind === 'tool') {
    return (
      <>
        <label className="field">官网
          <input value={details.website_url} onChange={(event) => onDetails({ website_url: event.target.value })} />
        </label>
        <label className="field">收费
          <select value={details.pricing} onChange={(event) => onDetails({ pricing: event.target.value })}>
            {PRICING.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <fieldset className="field">
          <legend>平台</legend>
          <div className="checks">
            {PLATFORMS.map(([value, label]) => (
              <label key={value}>
                <input
                  type="checkbox"
                  checked={details.platforms.includes(value)}
                  onChange={() => onDetails({ platforms: toggle(details.platforms, value) })}
                />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
        <fieldset className="field">
          <legend>部署</legend>
          <div className="checks">
            {DEPLOYMENTS.map(([value, label]) => (
              <label key={value}>
                <input
                  type="checkbox"
                  checked={details.deployment.includes(value)}
                  onChange={() => onDetails({ deployment: toggle(details.deployment, value) })}
                />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
      </>
    )
  }
  if (form.kind === 'tutorial') {
    return (
      <>
        <label className="field">难度
          <select value={details.level} onChange={(event) => onDetails({ level: event.target.value })}>
            {LEVELS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <label className="field">分钟
          <input type="number" min="0" value={details.minutes} onChange={(event) => onDetails({ minutes: Number(event.target.value) })} />
        </label>
        <label className="field">Markdown 正文
          <textarea rows={8} value={form.bodyMarkdown} onChange={(event) => onDetails({ __body: event.target.value })} />
        </label>
        <div className="field">
          <span>步骤</span>
          <div className="steps-editor">
            {details.steps.map((step, index) => (
              <div className="step-row" key={index}>
                <input
                  aria-label={`步骤 ${index + 1}`}
                  value={step}
                  onChange={(event) => onDetails({ steps: replaceAt(details.steps, index, event.target.value) })}
                />
                <span className="row-actions">
                  <button type="button" className="pill" onClick={() => onDetails({ steps: move(details.steps, index, -1) })}>上移</button>
                  <button type="button" className="pill" onClick={() => onDetails({ steps: move(details.steps, index, 1) })}>下移</button>
                  <button type="button" className="pill" onClick={() => onDetails({ steps: details.steps.filter((_, i) => i !== index) })}>删除</button>
                </span>
              </div>
            ))}
            <button type="button" className="pill" onClick={() => onDetails({ steps: details.steps.concat('') })}>添加步骤</button>
          </div>
          <p className="hint">正文和步骤至少填写一项。</p>
        </div>
        <label className="field">作者
          <input value={details.author} onChange={(event) => onDetails({ author: event.target.value })} />
        </label>
        <label className="field">原文链接
          <input value={details.source_url} onChange={(event) => onDetails({ source_url: event.target.value })} />
        </label>
        <label className="field">注意
          <textarea rows={3} value={details.notes} onChange={(event) => onDetails({ notes: event.target.value })} />
        </label>
      </>
    )
  }
  return (
    <>
      <label className="field">owner/name
        <input value={details.full_name} onChange={(event) => onDetails({ full_name: event.target.value })} />
      </label>
      <label className="field">语言
        <input value={details.language} onChange={(event) => onDetails({ language: event.target.value })} />
      </label>
      <label className="field">许可证
        <input value={details.license} onChange={(event) => onDetails({ license: event.target.value })} />
      </label>
      <label className="lock-line">
        <input type="checkbox" checked={details.archived} onChange={(event) => onDetails({ archived: event.target.checked })} />
        已归档
      </label>
      <label className="field">最近活动时间（UTC）
        <input type="datetime-local" value={details.last_activity_at} onChange={(event) => onDetails({ last_activity_at: event.target.value })} />
      </label>
      <label className="field">仓库数字 ID
        <input readOnly value={details.github_repository_id} />
      </label>
      {!details.github_repository_id && <p className="hint">等待采集确认</p>}
    </>
  )
}

export function applyDetailsChange(form, patch) {
  if (Object.prototype.hasOwnProperty.call(patch, '__body')) {
    return { ...form, bodyMarkdown: patch.__body }
  }
  const next = { ...patch }
  if (Object.prototype.hasOwnProperty.call(next, 'last_activity_at')) next.last_activity_at = next.last_activity_at
  return { ...form, details: { ...form.details, ...next } }
}

function toggle(list, value) {
  return list.includes(value) ? list.filter((item) => item !== value) : list.concat(value)
}

function replaceAt(list, index, value) {
  return list.map((item, i) => (i === index ? value : item))
}

function move(list, index, delta) {
  const target = index + delta
  if (target < 0 || target >= list.length) return list
  const next = list.slice()
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}
