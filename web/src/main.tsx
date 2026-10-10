import '@fontsource-variable/outfit'
import '@fontsource-variable/work-sans'
import '@fontsource-variable/jetbrains-mono'
import '@fontsource-variable/vazirmatn'
import './styles.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
