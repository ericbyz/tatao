export function StatusTag({ tone, children }: { tone: 'success' | 'warning' | 'neutral' | 'danger'; children: React.ReactNode }) {
  return <span className={`status-tag status-tag--${tone}`}>{children}</span>
}

