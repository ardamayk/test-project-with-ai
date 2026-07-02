import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from '@tanstack/react-router'
import {
  AudioLines,
  Check,
  ChevronRight,
  Disc3,
  Download,
  Infinity as InfinityIcon,
  Info,
  ListMusic,
  MoreVertical,
  Pause,
  Play,
  Plus,
  Repeat,
  Shuffle,
  SkipBack,
  SkipForward,
  Volume2,
  X,
} from 'lucide-react'
import type { Playlist, Track } from '@repo/api-client'
import { usePlayback } from '../playback/PlaybackProvider'
import { useLayout } from './LayoutProvider'
import { getQueuePanel } from '../widgets/layout-utils'
import { AlbumArt } from './AlbumArt'
import { cn } from '../lib/utils'

const RECENT_PLAYLISTS_KEY = 'navidrome-recent-playlists'
const RECENT_PLAYLIST_LIMIT = 2

function readRecentPlaylistIds(): string[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(RECENT_PLAYLISTS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed)
      ? parsed.filter((id): id is string => typeof id === 'string')
      : []
  } catch {
    return []
  }
}

function touchRecentPlaylist(playlistId: string) {
  if (typeof window === 'undefined') return
  const recent = readRecentPlaylistIds().filter((id) => id !== playlistId)
  recent.unshift(playlistId)
  window.localStorage.setItem(
    RECENT_PLAYLISTS_KEY,
    JSON.stringify(recent.slice(0, 10)),
  )
}

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

function formatSampleRate(hz?: number): string | null {
  if (!hz || hz <= 0) return null
  if (hz % 1000 === 0) return `${hz / 1000} kHz`
  return `${(hz / 1000).toFixed(1)} kHz`
}

function formatQualityLabel(track: { bitrateKbps?: number; sampleRateHz?: number } | null): string {
  if (!track) return 'Quality'
  const parts = [
    track.bitrateKbps && track.bitrateKbps > 0
      ? `${track.bitrateKbps} kbps`
      : null,
    formatSampleRate(track.sampleRateHz),
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : 'Quality'
}

function formatDuration(ms?: number): string | null {
  if (!ms || ms <= 0) return null
  const total = Math.floor(ms / 1000)
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${minutes}m ${seconds}s`
}

function formatBytes(bytes?: number): string | null {
  if (!bytes || bytes <= 0) return null
  const mib = bytes / 1024 / 1024
  return `${mib.toFixed(2)} MiB`
}

function isLosslessFormat(format?: string): boolean {
  return ['flac', 'alac', 'wav', 'aiff', 'dsd'].includes(
    format?.toLowerCase() ?? '',
  )
}

type MenuPosition = {
  top: number
  left: number
}

export function PlayerBar({
  onPlaylistMutated,
}: {
  onPlaylistMutated?: () => void
} = {}) {
  const navigate = useNavigate()
  const actionsButtonRef = useRef<HTMLButtonElement>(null)
  const [actionsOpen, setActionsOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState<MenuPosition | null>(null)
  const [playlistSubmenuOpen, setPlaylistSubmenuOpen] = useState(false)
  const [infoOpen, setInfoOpen] = useState(false)
  const [playlists, setPlaylists] = useState<Playlist[]>([])
  const [playlistsLoaded, setPlaylistsLoaded] = useState(false)
  const [memberPlaylistIds, setMemberPlaylistIds] = useState<Set<string>>(
    () => new Set(),
  )
  const [playlistQuery, setPlaylistQuery] = useState('')
  const [createPlaylistOpen, setCreatePlaylistOpen] = useState(false)
  const [newPlaylistName, setNewPlaylistName] = useState('')
  const { preferences, togglePanel } = useLayout()
  const queuePanelSide = getQueuePanel(preferences.layout.sidebarPosition)
  const {
    currentTrack,
    isPlaying,
    currentTime,
    duration,
    volume,
    shuffleEnabled,
    repeatMode,
    togglePlay,
    toggleShuffle,
    cycleRepeatMode,
    seek,
    setVolume,
    playQueueIndex,
    playNext,
    queue,
    getAlbumCoverUrl,
    listPlaylists,
    getPlaylist,
    createPlaylist,
    addPlaylistTrack,
    removePlaylistTrack,
  } = usePlayback()

  const updateMenuPosition = useCallback(() => {
    const button = actionsButtonRef.current
    if (!button) return
    const rect = button.getBoundingClientRect()
    setMenuPosition({
      top: rect.top - 8,
      left: rect.left,
    })
  }, [])

  useEffect(() => {
    if (!actionsOpen) return
    updateMenuPosition()
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target
      if (!(target instanceof Node)) return
      if (actionsButtonRef.current?.contains(target)) return
      const menu = document.getElementById('player-track-actions-menu')
      if (menu?.contains(target)) return
      setActionsOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPlaylistSubmenuOpen(false)
        setActionsOpen(false)
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    window.addEventListener('resize', updateMenuPosition)
    window.addEventListener('scroll', updateMenuPosition, true)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('resize', updateMenuPosition)
      window.removeEventListener('scroll', updateMenuPosition, true)
    }
  }, [actionsOpen, updateMenuPosition])

  const loadPlaylistMembership = useCallback(
    async (items: Playlist[], trackId: string) => {
      const details = await Promise.all(
        items.map((playlist) => getPlaylist(playlist.id)),
      )
      const memberIds = new Set<string>()
      for (const detail of details) {
        if (detail.tracks.some((entry) => entry.id === trackId)) {
          memberIds.add(detail.id)
        }
      }
      setMemberPlaylistIds(memberIds)
    },
    [getPlaylist],
  )

  const loadPlaylistsForSubmenu = useCallback(async () => {
    if (!currentTrack) return
    const data = await listPlaylists()
    setPlaylists(data.items)
    await loadPlaylistMembership(data.items, currentTrack.id)
    setPlaylistsLoaded(true)
  }, [currentTrack, listPlaylists, loadPlaylistMembership])

  useEffect(() => {
    if (!actionsOpen || !currentTrack) {
      setPlaylistSubmenuOpen(false)
      setPlaylistQuery('')
      setCreatePlaylistOpen(false)
      setNewPlaylistName('')
      setPlaylistsLoaded(false)
      return
    }
    void loadPlaylistsForSubmenu()
  }, [actionsOpen, currentTrack, loadPlaylistsForSubmenu])

  const currentIndex = queue.findIndex(
    (item) => item.track.id === currentTrack?.id,
  )

  const effectiveDuration =
    duration > 0
      ? duration
      : currentTrack?.durationMs
        ? currentTrack.durationMs / 1000
        : 0

  const handleSeek = (value: number) => {
    if (effectiveDuration > 0) {
      seek(value * effectiveDuration)
    }
  }

  const qualityLabel = formatQualityLabel(currentTrack)
  const QualityIcon = isLosslessFormat(currentTrack?.format)
    ? Disc3
    : AudioLines
  const controlButtonClass =
    'inline-flex size-8 items-center justify-center rounded-full text-caption hover:bg-muted hover:text-foreground disabled:opacity-40'
  const activeControlButtonClass =
    'bg-primary/15 text-heading ring-1 ring-primary/30'
  const sortedPlaylists = useMemo(
    () =>
      [...playlists].sort((a, b) => {
        if (a.isDefault !== b.isDefault) return a.isDefault ? -1 : 1
        return a.name.localeCompare(b.name)
      }),
    [playlists],
  )

  const playlistSearchQuery = playlistQuery.trim().toLowerCase()

  const visiblePlaylists = useMemo(() => {
    if (playlistSearchQuery) {
      return sortedPlaylists.filter((playlist) =>
        playlist.name.toLowerCase().includes(playlistSearchQuery),
      )
    }
    const recentIds = readRecentPlaylistIds()
    const recent = recentIds
      .map((id) => sortedPlaylists.find((playlist) => playlist.id === id))
      .filter((playlist): playlist is Playlist => Boolean(playlist))
      .slice(0, RECENT_PLAYLIST_LIMIT)
    if (recent.length >= RECENT_PLAYLIST_LIMIT) return recent
    const recentSet = new Set(recent.map((playlist) => playlist.id))
    for (const playlist of sortedPlaylists) {
      if (recent.length >= RECENT_PLAYLIST_LIMIT) break
      if (!recentSet.has(playlist.id)) recent.push(playlist)
    }
    return recent.slice(0, RECENT_PLAYLIST_LIMIT)
  }, [playlistSearchQuery, sortedPlaylists])

  const closeActionsMenu = () => {
    setPlaylistSubmenuOpen(false)
    setActionsOpen(false)
  }

  const openPlaylistSubmenu = () => {
    setPlaylistSubmenuOpen(true)
    if (!playlistsLoaded) void loadPlaylistsForSubmenu()
  }

  const closePlaylistSubmenu = () => {
    setPlaylistSubmenuOpen(false)
    setPlaylistQuery('')
    setCreatePlaylistOpen(false)
    setNewPlaylistName('')
  }

  const openInfoModal = () => {
    closeActionsMenu()
    setInfoOpen(true)
  }

  const handlePlayNext = () => {
    if (!currentTrack) return
    closeActionsMenu()
    void playNext(currentTrack.id)
  }

  const handleGoToAlbum = () => {
    if (!currentTrack) return
    closeActionsMenu()
    void navigate({
      to: '/library/$albumId',
      params: { albumId: currentTrack.albumId },
    })
  }

  const handleGoToArtist = () => {
    if (!currentTrack) return
    closeActionsMenu()
    void navigate({
      to: '/library/artists',
      search: { q: currentTrack.artistName },
    })
  }

  const notifyPlaylistMutated = () => {
    onPlaylistMutated?.()
  }

  const refreshPlaylists = async () => {
    const data = await listPlaylists()
    setPlaylists(data.items)
    if (currentTrack) {
      await loadPlaylistMembership(data.items, currentTrack.id)
    }
    notifyPlaylistMutated()
  }

  const handleTogglePlaylist = async (playlistId: string) => {
    if (!currentTrack) return
    if (memberPlaylistIds.has(playlistId)) {
      await removePlaylistTrack(playlistId, currentTrack.id)
      setMemberPlaylistIds((current) => {
        const next = new Set(current)
        next.delete(playlistId)
        return next
      })
    } else {
      await addPlaylistTrack(playlistId, currentTrack.id)
      setMemberPlaylistIds((current) => new Set(current).add(playlistId))
      touchRecentPlaylist(playlistId)
    }
    const data = await listPlaylists()
    setPlaylists(data.items)
    notifyPlaylistMutated()
  }

  const handleCreatePlaylist = async () => {
    if (!currentTrack || !newPlaylistName.trim()) return
    const playlist = await createPlaylist(newPlaylistName.trim())
    await addPlaylistTrack(playlist.id, currentTrack.id)
    touchRecentPlaylist(playlist.id)
    setNewPlaylistName('')
    setCreatePlaylistOpen(false)
    await refreshPlaylists()
  }

  return (
    <footer className="border-border border-t bg-player text-player-foreground backdrop-blur supports-[backdrop-filter]:bg-player/95">
      <div className="grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 px-3 py-2 sm:gap-4 sm:px-4 sm:py-3">
        <section
          aria-label="Now playing"
          className="flex w-full max-w-[18rem] min-w-0 items-center gap-2 justify-self-start sm:gap-3"
        >
          <AlbumArt
            coverUrl={
              currentTrack ? getAlbumCoverUrl(currentTrack.albumId) : null
            }
            title={currentTrack?.title ?? '♪'}
            className="size-12 shrink-0 rounded-md text-sm shadow-sm sm:size-14 md:size-16"
          />
          <div className="min-w-0 flex-1">
            <div className="flex max-w-full min-w-0 items-center">
              <p
                className="min-w-0 truncate font-semibold text-heading text-sm"
                title={currentTrack?.title}
              >
                {currentTrack?.title ?? 'Nothing playing'}
              </p>
              <button
                ref={actionsButtonRef}
                type="button"
                className={cn(
                  'ml-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-full text-caption hover:bg-muted hover:text-heading focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:opacity-40',
                  actionsOpen && 'bg-muted text-heading',
                )}
                aria-label="Track actions"
                aria-expanded={actionsOpen}
                disabled={!currentTrack}
                onClick={() => {
                  setActionsOpen((open) => {
                    const next = !open
                    if (next) updateMenuPosition()
                    return next
                  })
                }}
              >
                <MoreVertical className="size-3.5" />
              </button>
            </div>
            <p
              className="truncate text-foreground text-xs"
              title={currentTrack?.artistName}
            >
              {currentTrack?.artistName ?? 'Select a track'}
            </p>
            {currentTrack?.albumTitle ? (
              <p
                className="hidden truncate text-caption text-xs sm:block"
                title={currentTrack.albumTitle}
              >
                {currentTrack.albumTitle}
              </p>
            ) : null}
            {actionsOpen && currentTrack && menuPosition ? (
              <Portal>
                <div
                  id="player-track-actions-menu"
                  className="fixed z-50 min-w-44 -translate-y-full rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg"
                  style={{
                    top: menuPosition.top,
                    left: menuPosition.left,
                  }}
                  role="menu"
                >
                  <AddToPlaylistMenuItem
                    open={playlistSubmenuOpen}
                    onOpen={openPlaylistSubmenu}
                    onClose={closePlaylistSubmenu}
                    query={playlistQuery}
                    onQueryChange={setPlaylistQuery}
                    createOpen={createPlaylistOpen}
                    onCreateOpen={() => setCreatePlaylistOpen(true)}
                    onCreateClose={() => {
                      setCreatePlaylistOpen(false)
                      setNewPlaylistName('')
                    }}
                    newPlaylistName={newPlaylistName}
                    onNewPlaylistNameChange={setNewPlaylistName}
                    playlists={visiblePlaylists}
                    memberPlaylistIds={memberPlaylistIds}
                    isSearching={Boolean(playlistSearchQuery)}
                    onToggle={(playlistId) => void handleTogglePlaylist(playlistId)}
                    onCreate={() => void handleCreatePlaylist()}
                  />
                  <MenuButton onClick={handlePlayNext}>
                    <SkipForward className="size-3.5" />
                    Play next
                  </MenuButton>
                  <MenuButton onClick={handleGoToAlbum}>
                    Go to album
                  </MenuButton>
                  <MenuButton onClick={handleGoToArtist}>
                    Go to artist
                  </MenuButton>
                  <MenuButton disabled>
                    <Download className="size-3.5" />
                    Download
                  </MenuButton>
                  <MenuButton onClick={openInfoModal}>
                    <Info className="size-3.5" />
                    Get Info
                  </MenuButton>
                </div>
              </Portal>
            ) : null}
          </div>
        </section>

        <section
          aria-label="Playback controls"
          className="flex w-[min(100%,20rem)] shrink-0 flex-col items-center gap-1.5 justify-self-center sm:w-[min(100%,24rem)] md:w-[min(100%,30rem)]"
        >
          <div className="flex items-center gap-0.5 sm:gap-1">
            <button
              type="button"
              className={cn(
                controlButtonClass,
                shuffleEnabled && activeControlButtonClass,
              )}
              aria-label={shuffleEnabled ? 'Shuffle on' : 'Shuffle off'}
              onClick={toggleShuffle}
              disabled={!currentTrack}
            >
              <Shuffle className="size-4" />
            </button>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted disabled:opacity-40"
              onClick={() => {
                if (currentIndex > 0) {
                  void playQueueIndex(currentIndex - 1)
                }
              }}
              disabled={currentIndex <= 0}
              aria-label="Previous"
            >
              <SkipBack className="size-4" />
            </button>
            <button
              type="button"
              className={cn(
                'inline-flex size-10 items-center justify-center rounded-full bg-primary text-primary-foreground hover:opacity-90 disabled:opacity-50',
              )}
              onClick={togglePlay}
              disabled={!currentTrack}
              aria-label={isPlaying ? 'Pause' : 'Play'}
            >
              {isPlaying ? (
                <Pause className="size-4" />
              ) : (
                <Play className="size-4" />
              )}
            </button>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted disabled:opacity-40"
              onClick={() => {
                if (currentIndex >= 0 && currentIndex < queue.length - 1) {
                  void playQueueIndex(currentIndex + 1)
                }
              }}
              disabled={currentIndex < 0 || currentIndex >= queue.length - 1}
              aria-label="Next"
            >
              <SkipForward className="size-4" />
            </button>
            <button
              type="button"
              className={cn(
                controlButtonClass,
                repeatMode !== 'off' && activeControlButtonClass,
                'relative',
              )}
              aria-label={
                repeatMode === 'off'
                  ? 'Repeat off'
                  : repeatMode === 'once'
                    ? 'Repeat once'
                    : 'Repeat loop'
              }
              onClick={cycleRepeatMode}
              disabled={!currentTrack}
            >
              <Repeat className="size-4" />
              {repeatMode === 'once' ? (
                <span className="-right-0.5 -bottom-0.5 absolute flex size-3 items-center justify-center rounded-full bg-primary font-semibold text-[0.5rem] text-primary-foreground">
                  1
                </span>
              ) : null}
              {repeatMode === 'loop' ? (
                <span
                  className="-right-1 -bottom-1 absolute flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground"
                  aria-label="Repeat infinitely"
                >
                  <InfinityIcon className="size-2.5" />
                </span>
              ) : null}
            </button>
          </div>
          <div className="flex w-full min-w-0 items-center gap-1.5 text-xs tabular-nums sm:gap-2">
            <span className="w-8 shrink-0 text-right text-caption sm:w-10">
              {formatTime(currentTime)}
            </span>
            <input
              type="range"
              min={0}
              max={1}
              step={0.001}
              value={
                effectiveDuration > 0 ? currentTime / effectiveDuration : 0
              }
              onChange={(e) => handleSeek(Number(e.target.value))}
              className="min-w-0 flex-1 accent-primary disabled:opacity-40"
              disabled={!currentTrack}
              aria-label="Seek"
            />
            <span className="w-8 shrink-0 text-caption sm:w-10">
              {formatTime(effectiveDuration)}
            </span>
          </div>
        </section>

        <section
          aria-label="Volume and queue"
          className="flex min-w-0 items-center justify-end gap-1 justify-self-end sm:gap-2"
        >
          <button
            type="button"
            className="hidden size-8 shrink-0 items-center justify-center rounded-full text-caption hover:bg-muted hover:text-foreground sm:inline-flex"
            onClick={() => togglePanel(queuePanelSide)}
            aria-label="Toggle queue panel"
          >
            <ListMusic className="size-4" />
          </button>
          <button
            type="button"
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full border border-border bg-background/40 px-2 text-caption text-xs hover:bg-muted disabled:opacity-80 sm:px-2.5"
            aria-label={`Quality ${qualityLabel}`}
            disabled
            title="Quality selector coming soon"
          >
            <QualityIcon className="size-3.5 shrink-0" />
            <span className="hidden font-medium tabular-nums md:inline">
              {qualityLabel}
            </span>
          </button>
          <Volume2 className="hidden size-4 shrink-0 text-caption sm:block" aria-hidden />
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={volume}
            onChange={(e) => setVolume(Number(e.target.value))}
            className="w-14 shrink-0 accent-primary sm:w-20 md:w-28"
            aria-label="Volume"
          />
        </section>
      </div>
      {infoOpen && currentTrack ? (
        <TrackInfoDialog
          track={currentTrack}
          onClose={() => setInfoOpen(false)}
        />
      ) : null}
    </footer>
  )
}

function Portal({ children }: { children: ReactNode }) {
  if (typeof document === 'undefined') return null
  return createPortal(children, document.body)
}

function AddToPlaylistMenuItem({
  open,
  onOpen,
  onClose,
  query,
  onQueryChange,
  createOpen,
  onCreateOpen,
  onCreateClose,
  newPlaylistName,
  onNewPlaylistNameChange,
  playlists,
  memberPlaylistIds,
  isSearching,
  onToggle,
  onCreate,
}: {
  open: boolean
  onOpen: () => void
  onClose: () => void
  query: string
  onQueryChange: (value: string) => void
  createOpen: boolean
  onCreateOpen: () => void
  onCreateClose: () => void
  newPlaylistName: string
  onNewPlaylistNameChange: (value: string) => void
  playlists: { id: string; name: string; isDefault: boolean; trackCount: number }[]
  memberPlaylistIds: Set<string>
  isSearching: boolean
  onToggle: (playlistId: string) => void
  onCreate: () => void
}) {
  return (
    <div
      className="relative"
      onMouseEnter={onOpen}
      onMouseLeave={onClose}
    >
      <div
        className={cn(
          'flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-xs',
          open && 'bg-muted text-heading',
        )}
        role="menuitem"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <span className="flex items-center gap-2">
          <Plus className="size-3.5" />
          Add to playlist
        </span>
        <ChevronRight className="size-3.5 text-caption" />
      </div>
      {open ? (
        <div
          className="absolute top-0 left-full z-50 ml-1 w-56 rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-lg"
          role="menu"
          aria-label="Add to playlist"
          onMouseEnter={onOpen}
          onMouseLeave={onClose}
        >
          <input
            type="search"
            placeholder="Search playlists"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            className="mb-2 h-8 w-full rounded-md border border-border bg-background px-2.5 text-xs outline-none focus:ring-2 focus:ring-primary/40"
            onClick={(event) => event.stopPropagation()}
          />
          {createOpen ? (
            <div className="mb-2 flex gap-1.5">
              <label className="sr-only" htmlFor="player-new-playlist-name">
                New playlist name
              </label>
              <input
                id="player-new-playlist-name"
                value={newPlaylistName}
                onChange={(event) => onNewPlaylistNameChange(event.target.value)}
                className="h-8 min-w-0 flex-1 rounded-md border border-border bg-background px-2.5 text-xs outline-none focus:ring-2 focus:ring-primary/40"
                placeholder="Playlist name"
                autoFocus
              />
              <button
                type="button"
                className="inline-flex h-8 items-center rounded-md bg-primary px-2.5 font-medium text-primary-foreground text-xs disabled:opacity-50"
                disabled={!newPlaylistName.trim()}
                onClick={onCreate}
              >
                Create
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="mb-2 flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted"
              role="menuitem"
              onClick={onCreateOpen}
            >
              <span className="inline-flex size-5 items-center justify-center rounded-full border border-border">
                <Plus className="size-3" />
              </span>
              Create new playlist
            </button>
          )}
          {!isSearching && playlists.length > 0 ? (
            <p className="mb-1 px-2 font-medium text-[0.625rem] text-caption uppercase tracking-wide">
              Recent
            </p>
          ) : null}
          <div className="max-h-40 overflow-auto">
            {playlists.length === 0 ? (
              <p className="px-2 py-1.5 text-caption text-xs">
                {isSearching ? 'No playlists found' : 'No playlists yet'}
              </p>
            ) : (
              playlists.map((playlist) => {
                const isMember = memberPlaylistIds.has(playlist.id)
                return (
                  <button
                    key={playlist.id}
                    type="button"
                    className="flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted"
                    role="menuitem"
                    aria-label={
                      isMember
                        ? `Remove from ${playlist.name}`
                        : `Add to ${playlist.name}`
                    }
                    onClick={() => onToggle(playlist.id)}
                  >
                    <span className="min-w-0 truncate">{playlist.name}</span>
                    {isMember ? (
                      <Check className="size-3.5 shrink-0 text-heading" />
                    ) : null}
                  </button>
                )
              })
            )}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function MenuButton({
  children,
  disabled = false,
  onClick,
}: {
  children: ReactNode
  disabled?: boolean
  onClick?: () => void
}) {
  return (
    <button
      type="button"
      className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
      role="menuitem"
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

function TrackInfoDialog({
  track,
  onClose,
}: {
  track: Track
  onClose: () => void
}) {
  const rows = [
    ['Title', track.title],
    ['Artist', track.artistName],
    ['Album', track.albumTitle],
    ['Track', track.trackNo?.toString()],
    ['Duration', formatDuration(track.durationMs)],
    ['Codec', track.format],
    ['Bitrate', track.bitrateKbps ? `${track.bitrateKbps} kbps` : null],
    ['Sample rate', formatSampleRate(track.sampleRateHz)],
    ['Bit depth', track.bitDepth ? `${track.bitDepth}-bit` : null],
    ['Genre', track.genre],
    ['Size', formatBytes(track.sizeBytes)],
    ['Id', track.id],
  ].filter((row): row is [string, string] => Boolean(row[1]))

  return (
    <Portal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4">
        <div
          role="dialog"
          aria-modal="true"
          aria-label={track.title}
          className="max-h-[80vh] w-full max-w-2xl overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-xl"
        >
          <div className="flex items-center justify-between gap-3 border-border border-b p-4">
            <h2 className="truncate font-semibold text-heading text-xl">
              {track.title}
            </h2>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted"
              aria-label="Close"
              onClick={onClose}
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="max-h-[65vh] overflow-auto p-4">
            {rows.map(([label, value]) => (
              <div
                key={label}
                className="grid grid-cols-[8rem_minmax(0,1fr)] gap-3 border-border border-b py-2 text-sm"
              >
                <span className="text-caption">{label}</span>
                <span className="min-w-0 break-words font-medium text-foreground">
                  {value}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </Portal>
  )
}
