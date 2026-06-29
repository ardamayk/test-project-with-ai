import { Link } from '@tanstack/react-router'
import {
  Folder,
  Heart,
  HelpCircle,
  ListMusic,
  Music2,
  Radio,
  Settings,
  SquareLibrary,
} from 'lucide-react'
import { AppBrand } from './AppBrand'
import { MiniPlayer } from './MiniPlayer'
import { PanelCollapseButton } from './PanelCollapseButton'
import { useLayout } from './LayoutProvider'
import { getNavPanel } from '../widgets/layout-utils'
import { cn } from '../lib/utils'

const libraryNav = [
  { to: '/favorites', label: 'Favorites', icon: Heart },
  { to: '/library/albums', label: 'Albums', icon: SquareLibrary },
  { to: '/folders', label: 'Folders', icon: Folder },
  { to: '/radio', label: 'Radio Stations', icon: Radio },
  { to: '/library/tracks', label: 'Tracks', icon: Music2 },
  { to: '/playlists', label: 'Playlists', icon: ListMusic },
] as const

const footerNav = [{ to: '/settings', label: 'Settings', icon: Settings }] as const

function NavLink({
  to,
  label,
  icon: Icon,
  compact = false,
}: {
  to: string
  label: string
  icon: typeof Heart
  compact?: boolean
}) {
  if (compact) {
    return (
      <Link
        to={to}
        title={label}
        className="flex size-9 items-center justify-center rounded-md border-l-2 border-transparent text-sidebar-foreground/85 transition hover:bg-sidebar-accent hover:text-sidebar-foreground [&.active]:border-primary [&.active]:bg-sidebar-accent [&.active]:text-primary"
      >
        <Icon className="size-4 shrink-0" />
      </Link>
    )
  }

  return (
    <Link
      to={to}
      className="flex items-center gap-2.5 whitespace-nowrap rounded-md border-l-2 border-transparent px-3 py-2.5 text-[0.9375rem] text-sidebar-foreground/85 transition hover:bg-sidebar-accent hover:text-sidebar-foreground [&.active]:border-primary [&.active]:bg-sidebar-accent [&.active]:font-medium [&.active]:text-primary"
    >
      <Icon className="size-4 shrink-0" />
      {label}
    </Link>
  )
}

export function SidebarNav() {
  const { preferences, togglePanel } = useLayout()
  const panelSide = getNavPanel(preferences.layout.sidebarPosition)
  const isCollapsed = preferences.layout.collapsed[panelSide]

  if (isCollapsed) {
    return (
      <div className="flex h-full flex-col items-center bg-sidebar text-sidebar-foreground">
        <div className="flex w-full justify-center px-1 pt-2">
          <PanelCollapseButton
            edge={panelSide}
            collapsed
            onToggle={() => togglePanel(panelSide)}
          />
        </div>
        <AppBrand compact />
        <nav className="flex flex-1 flex-col items-center gap-2 overflow-y-auto py-2">
          {libraryNav.map((item) => (
            <NavLink key={item.to} {...item} compact />
          ))}
          <div className="my-2 w-6 border-sidebar-border border-t" />
          {footerNav.map((item) => (
            <NavLink key={item.to} {...item} compact />
          ))}
        </nav>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <div
        className={cn(
          'flex items-start gap-1 border-sidebar-border border-b px-2 py-2',
          panelSide === 'right' && 'flex-row-reverse',
        )}
      >
        <AppBrand />
        <PanelCollapseButton
          edge={panelSide}
          collapsed={false}
          onToggle={() => togglePanel(panelSide)}
          className="mt-1"
        />
      </div>
      <nav className="flex flex-1 flex-col gap-1.5 overflow-y-auto px-2 py-1">
        {libraryNav.map((item) => (
          <NavLink key={item.to} {...item} />
        ))}
        <div className="my-3 border-sidebar-border border-t" />
        {footerNav.map((item) => (
          <NavLink key={item.to} {...item} />
        ))}
      </nav>
      <div className="mt-auto border-sidebar-border border-t p-3">
        <MiniPlayer />
        <button
          type="button"
          disabled
          className="mt-2 flex w-full items-center gap-2 px-2 py-1 text-muted-foreground text-xs"
        >
          <HelpCircle className="size-3.5" />
          Help (soon)
        </button>
      </div>
    </div>
  )
}
