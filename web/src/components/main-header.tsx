import { Link, useRouterState } from '@tanstack/react-router'
import { Bell, Search } from 'lucide-react'
import { Avatar, AvatarFallback } from '#/components/ui/avatar'
import { Input } from '#/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '#/components/ui/tabs'

const libraryTabs = [
  { to: '/library/albums', label: 'Albums' },
  { to: '/library/artists', label: 'Artists' },
  { to: '/library/genres', label: 'Genres' },
] as const

export function MainHeader({
  search,
  onSearchChange,
}: {
  search?: string
  onSearchChange?: (value: string) => void
}) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const activeTab =
    libraryTabs.find((tab) => pathname.startsWith(tab.to))?.to ??
    '/library/albums'

  return (
    <header className="sticky top-0 z-10 border-border border-b bg-card/90 px-6 py-4 text-card-foreground backdrop-blur">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <div className="relative min-w-0 flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
            <Input
              className="pl-9"
              placeholder="Search your library…"
              value={search ?? ''}
              onChange={(e) => onSearchChange?.(e.target.value)}
            />
          </div>
          <button
            type="button"
            className="text-caption hover:text-foreground"
            aria-label="Notifications"
          >
            <Bell className="size-5" />
          </button>
          <Avatar className="size-8">
            <AvatarFallback>JD</AvatarFallback>
          </Avatar>
        </div>
        <Tabs value={activeTab}>
          <TabsList>
            {libraryTabs.map((tab) => (
              <TabsTrigger key={tab.to} value={tab.to} asChild>
                <Link to={tab.to}>{tab.label}</Link>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
    </header>
  )
}
