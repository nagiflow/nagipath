import { Route, Routes } from 'react-router-dom'
import { FoundationCheck } from '../pages/foundation-check/FoundationCheck'

// Phase 0 placeholder: proves the build pipeline, EUI, and the session
// bootstrap endpoint work end to end. Replaced page by page starting Phase 1
// (see docs/adr/0017-react-spa-with-eui-supersedes-0013.md).
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<FoundationCheck />} />
    </Routes>
  )
}
