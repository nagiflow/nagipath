import type { ButtonHTMLAttributes, ReactNode } from 'react'
import './Button.css'

export function Button({
  children, primary, subtle, small, danger, disabled, loading, onClick, href, type, className,
}: {
  children: ReactNode
  primary?: boolean
  subtle?: boolean
  small?: boolean
  danger?: boolean
  disabled?: boolean
  loading?: boolean
  onClick?: () => void
  href?: string
  type?: ButtonHTMLAttributes<HTMLButtonElement>['type']
  className?: string
}) {
  const cls = `btn${primary ? ' p' : ''}${subtle ? ' s' : ''}${small ? ' sm' : ''}${danger ? ' danger' : ''}${className ? ` ${className}` : ''}`
  if (href) return <a className={cls} href={href}>{children}</a>
  return (
    <button type={type ?? 'button'} className={cls} disabled={disabled || loading} onClick={onClick}>
      {loading ? '…' : children}
    </button>
  )
}
