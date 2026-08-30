import type { ReactNode } from 'react'
import { Button } from './Button'
import { Modal } from './Modal'

export function ConfirmModal({ title, body, onCancel, onConfirm, danger, loading, confirmLabel = 'Confirm' }: {
  title: ReactNode
  body: ReactNode
  onCancel: () => void
  onConfirm: () => void
  danger?: boolean
  loading?: boolean
  confirmLabel?: string
}) {
  return (
    <Modal
      title={title}
      onClose={onCancel}
      footer={
        <>
          <Button subtle onClick={onCancel}>Cancel</Button>
          <Button primary={!danger} danger={danger} loading={loading} onClick={onConfirm}>{confirmLabel}</Button>
        </>
      }
    >
      <div className="m mu">{body}</div>
    </Modal>
  )
}
