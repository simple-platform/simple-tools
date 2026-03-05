import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { loadTheme } from './lib/simple'
import './styles/theme.css'

// Load tenant-specific theme overrides
loadTheme()

ReactDOM.createRoot(document.getElementById('space-root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
