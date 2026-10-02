export function openOutbound(client, resourceId, href) {
  if (!href) return
  const id = crypto.randomUUID()
  client.postEvents([{ id, resource_id: resourceId, type: 'outbound_click' }]).catch(() => {})
  window.open(href, '_blank', 'noopener,noreferrer')
}
