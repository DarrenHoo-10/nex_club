import { RouterProvider } from 'react-router-dom'
import { createAppRouter } from './routes.jsx'

const router = createAppRouter()

export default function App() {
  return <RouterProvider router={router} />
}
