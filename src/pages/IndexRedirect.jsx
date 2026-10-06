import { Navigate, useLocation } from 'react-router-dom'
import { DEFAULT_SECTION_KEY, legacySection } from '../sections.js'

export default function IndexRedirect() {
  const { hash } = useLocation()
  const key = legacySection(hash)
  return <Navigate to={`/${key || DEFAULT_SECTION_KEY}`} replace />
}
