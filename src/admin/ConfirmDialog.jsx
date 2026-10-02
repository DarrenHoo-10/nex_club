export default function ConfirmDialog({ open, title, children, confirmLabel, onConfirm, onClose, confirmDisabled = false }) {
  if (!open) return null
  return (
    <dialog open className="glass confirm" aria-labelledby="confirm-title">
      <h3 id="confirm-title">{title}</h3>
      {children}
      <div className="confirm-actions">
        <button type="button" className="pill" onClick={onClose}>取消</button>
        <button type="button" className="btn primary" onClick={onConfirm} disabled={confirmDisabled}>{confirmLabel}</button>
      </div>
    </dialog>
  )
}
