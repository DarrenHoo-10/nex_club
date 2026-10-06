const PREFIX = 'nex-reading:'

// Reading remains available when storage is blocked or full (e.g. private mode).
export function readPosition(key) {
  try {
    const value = JSON.parse(sessionStorage.getItem(PREFIX + key))
    return value && typeof value === 'object' ? value : null
  } catch {
    return null
  }
}

export function writePosition(key, value) {
  try { sessionStorage.setItem(PREFIX + key, JSON.stringify(value)) } catch { /* optional */ }
}
