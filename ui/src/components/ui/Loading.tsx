// Uses .m/.mus (Mono.css) only — no CSS class of its own.
export function Loading({ label }: { label?: string }) {
  return <div className="m mus" style={{ padding: 24, textAlign: 'center' }}>{label ?? 'Loading…'}</div>
}
