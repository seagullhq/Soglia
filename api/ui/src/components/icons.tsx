type IconProps = { size?: number; className?: string }

export function ShieldMark({ size = 26, className }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="M12 2.5 20 5.6v6.1c0 5-3.2 8.5-8 9.8-4.8-1.3-8-4.8-8-9.8V5.6L12 2.5Z" stroke="currentColor" strokeWidth="1.5" />
      <path d="M6.5 13.2h11" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="15.7" cy="13.2" r="1.7" fill="currentColor" />
    </svg>
  )
}

export function RefreshIcon({ size = 15, className }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="M20 11a8 8 0 1 0-2.3 6.1" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
      <path d="M20 4.5V11h-6.5" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function LayersIcon({ size = 15, className }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path d="m12 3 9 4.5-9 4.5-9-4.5L12 3Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <path d="m3 12 9 4.5 9-4.5M3 16.5 12 21l9-4.5" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
    </svg>
  )
}
