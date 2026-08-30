import './CodeBlock.css'

export function CodeBlock({ lines, startLine = 1, highlight }: { lines: string[]; startLine?: number; highlight?: (n: number) => boolean }) {
  return (
    <div className="code">
      {lines.map((line, i) => {
        const n = startLine + i
        return (
          <div className={`cl${highlight?.(n) ? ' on' : ''}`} key={i}>
            <span className="no">{n}</span>
            <span>{line}</span>
          </div>
        )
      })}
    </div>
  )
}
