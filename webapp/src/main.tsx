import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import Providers from './Providers'

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Could not find root element')
}

ReactDOM.createRoot(rootElement).render(
  <StrictMode>
    <Providers />
  </StrictMode>
)
