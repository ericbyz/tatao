import type { SVGProps } from 'react'

export type IconName =
  | 'home'
  | 'folder'
  | 'server'
  | 'clipboard'
  | 'clock'
  | 'settings'
  | 'search'
  | 'bell'
  | 'plus'
  | 'chevron'
  | 'close'
  | 'dots'
  | 'refresh'
  | 'check'
  | 'link'
  | 'bug'
  | 'file'
  | 'terminal'
  | 'lock'
  | 'menu'

const paths: Record<IconName, React.ReactNode> = {
  home: <><path d="m3 10 9-7 9 7"/><path d="M5 9v11h14V9"/><path d="M9 20v-6h6v6"/></>,
  folder: <path d="M3 6h6l2 2h10v11H3z"/>,
  server: <><rect x="3" y="4" width="18" height="6" rx="1"/><rect x="3" y="14" width="18" height="6" rx="1"/><path d="M7 7h.01M7 17h.01M11 7h6M11 17h6"/></>,
  clipboard: <><path d="M9 5H5v16h14V5h-4"/><rect x="9" y="3" width="6" height="4" rx="1"/><path d="M9 12h6M9 16h6"/></>,
  clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v6l4 2"/></>,
  settings: <><circle cx="12" cy="12" r="3"/><path d="M19 13.5a7.6 7.6 0 0 0 0-3l2-1.4-2-3.4-2.3 1A8 8 0 0 0 14 5.2L13.7 3h-4L9.3 5.2a8 8 0 0 0-2.6 1.5l-2.3-1-2 3.4 2 1.4a7.6 7.6 0 0 0 0 3l-2 1.4 2 3.4 2.3-1A8 8 0 0 0 9.3 19l.4 2h4l.4-2.2a8 8 0 0 0 2.6-1.5l2.3 1 2-3.4z"/></>,
  search: <><circle cx="11" cy="11" r="7"/><path d="m16 16 5 5"/></>,
  bell: <><path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9"/><path d="M10 21h4"/></>,
  plus: <path d="M12 5v14M5 12h14"/>,
  chevron: <path d="m9 7 5 5-5 5"/>,
  close: <path d="m6 6 12 12M18 6 6 18"/>,
  dots: <path d="M5 12h.01M12 12h.01M19 12h.01"/>,
  refresh: <><path d="M20 7v5h-5"/><path d="M4 17v-5h5"/><path d="M6.1 9A7 7 0 0 1 18.5 6L20 12M4 12l1.5 6A7 7 0 0 0 18 15"/></>,
  check: <path d="m5 12 4 4L19 6"/>,
  link: <><path d="m9 15-2 2a4 4 0 0 1-6-6l3-3a4 4 0 0 1 6 0"/><path d="m15 9 2-2a4 4 0 0 1 6 6l-3 3a4 4 0 0 1-6 0M8 12h8"/></>,
  bug: <><path d="M8 9h8v7a4 4 0 0 1-8 0zM9 5l2 3M15 5l-2 3M4 13h4M16 13h4M5 18l3-2M19 18l-3-2"/></>,
  file: <><path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h5M9 13h6M9 17h6"/></>,
  terminal: <><rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 15h4"/></>,
  lock: <><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></>,
  menu: <path d="M4 7h16M4 12h16M4 17h16"/>,
}

export function Icon({ name, size = 20, ...props }: { name: IconName; size?: number } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      aria-hidden="true"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...props}
    >
      {paths[name]}
    </svg>
  )
}

