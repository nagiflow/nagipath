// The product mark: an N built from a routed path, a node at each vertex —
// same 32×32/stroke=currentColor convention as icons.tsx, sized and coloured
// by its container. Also the source for public/favicon.svg (baked to a fixed
// colour there, since a favicon has no CSS context to inherit from).
export function Mark() {
  return (
    <svg viewBox="0 0 32 32" fill="none" width="100%" height="100%">
      <path d="M8 24 8 8 24 24 24 8" stroke="currentColor" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="8" cy="24" r="3.1" fill="currentColor" />
      <circle cx="8" cy="8" r="3.1" fill="currentColor" />
      <circle cx="24" cy="24" r="3.1" fill="currentColor" />
      <circle cx="24" cy="8" r="3.1" fill="currentColor" />
    </svg>
  )
}
