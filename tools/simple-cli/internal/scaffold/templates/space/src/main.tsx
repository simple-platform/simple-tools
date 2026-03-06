import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { loadTheme } from './lib/simple'
import './styles/theme.css'

function Root() {
  const [themeLoaded, setThemeLoaded] = React.useState(false)

  React.useEffect(() => {
    loadTheme().finally(() => setThemeLoaded(true))
  }, [])

  if (!themeLoaded) {
    return null // Or a loading spinner
  }

  return (
    <React.StrictMode>
      <App />
    </React.StrictMode>
  )
}

ReactDOM.createRoot(document.getElementById('space-root')!).render(<Root />)
