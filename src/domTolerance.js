// Some mobile browsers and translators reparent text and media nodes.
// React then throws removeChild and the router replaces the page with the
// application error. Skip the DOM call when the node has already moved.
let installed = false

export function tolerateDetachedDom() {
  if (installed || typeof Node !== 'function' || !Node.prototype) return
  installed = true
  const removeChild = Node.prototype.removeChild
  const insertBefore = Node.prototype.insertBefore
  Node.prototype.removeChild = function (child) {
    if (child.parentNode !== this) return child
    return removeChild.call(this, child)
  }
  Node.prototype.insertBefore = function (node, before) {
    if (before && before.parentNode !== this) return node
    return insertBefore.call(this, node, before)
  }
}
