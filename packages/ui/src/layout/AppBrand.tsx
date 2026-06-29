export function AppBrand({ compact = false }: { compact?: boolean }) {
  if (compact) {
    return (
      <div className="flex justify-center py-1" title="Earthly Audio">
        <div className="flex size-9 items-center justify-center rounded-lg bg-primary/15 text-primary">
          <span className="display-title font-semibold text-sm">E</span>
        </div>
      </div>
    )
  }

  return (
    <div className="min-w-0 flex-1 px-1 py-1">
      <p className="display-title font-semibold text-base text-sidebar-foreground">
        Earthly Audio
      </p>
      <p className="text-sidebar-foreground/70 text-xs">The Modern Craftsman</p>
    </div>
  )
}
