import { Navigate, useLocation } from 'react-router-dom'
import { legacySection } from '../sections.js'

export default function IndexRedirect() {
  const { hash } = useLocation()
  const key = legacySection(hash)
  return <Navigate to={key ? `/${key}` : '/tools'} replace />
}
