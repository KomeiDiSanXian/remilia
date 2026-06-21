export function SkeletonCard() {
  return (
    <div className="skeleton-card">
      <div className="skeleton-line skeleton-line-title" />
      <div className="skeleton-line skeleton-line-text" />
      <div className="skeleton-line skeleton-line-text skeleton-line-short" />
      <div className="skeleton-line skeleton-line-text" />
    </div>
  )
}

export function SkeletonBlock() {
  return (
    <div className="skeleton-block">
      <div className="skeleton-line skeleton-line-title" />
      <div className="skeleton-line skeleton-line-text" />
      <div className="skeleton-line skeleton-line-text skeleton-line-short" />
    </div>
  )
}

export function SkeletonGrid({ count = 6 }: { count?: number }) {
  return (
    <div className="plugin-grid">
      {Array.from({ length: count }).map((_, i) => <SkeletonCard key={i} />)}
    </div>
  )
}
