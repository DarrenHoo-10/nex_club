import React from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.jsx'
import { tolerateDetachedDom } from './domTolerance.js'
import './styles.css'

tolerateDetachedDom()

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
