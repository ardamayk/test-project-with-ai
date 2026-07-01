import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import type { Queue, QueueItem, Track } from '@repo/api-client'
import type { Playlist, PlaylistDetail, PlaylistList } from '@repo/api-client'

export type RepeatMode = 'off' | 'once' | 'loop'

export type PlaybackApi = {
  getQueue: () => Promise<Queue>
  replaceQueue: (trackIds: string[]) => Promise<Queue>
  appendQueueItem: (trackId: string) => Promise<Queue>
  removeQueueItem: (itemId: string) => Promise<Queue>
  getStreamUrl: (trackId: string) => string
  getAlbumCoverUrl: (albumId: string) => string
  listPlaylists: () => Promise<PlaylistList>
  getPlaylist: (playlistId: string) => Promise<PlaylistDetail>
  createPlaylist: (name: string) => Promise<Playlist>
  addPlaylistTrack: (playlistId: string, trackId: string) => Promise<PlaylistDetail>
  removePlaylistTrack: (playlistId: string, trackId: string) => Promise<PlaylistDetail>
}

type PlaybackContextValue = {
  queue: QueueItem[]
  currentTrack: Track | null
  isPlaying: boolean
  currentTime: number
  duration: number
  volume: number
  shuffleEnabled: boolean
  repeatMode: RepeatMode
  playTrack: (trackId: string, queueTrackIds?: string[]) => Promise<void>
  queueTracks: (trackIds: string[]) => Promise<void>
  playQueueIndex: (index: number) => Promise<void>
  playNext: (trackId: string) => Promise<void>
  togglePlay: () => void
  toggleShuffle: () => void
  cycleRepeatMode: () => void
  seek: (seconds: number) => void
  setVolume: (value: number) => void
  removeFromQueue: (itemId: string) => Promise<void>
  clearQueue: () => Promise<void>
  refreshQueue: () => Promise<void>
  getAlbumCoverUrl: (albumId: string) => string
  listPlaylists: () => Promise<PlaylistList>
  getPlaylist: (playlistId: string) => Promise<PlaylistDetail>
  createPlaylist: (name: string) => Promise<Playlist>
  addPlaylistTrack: (playlistId: string, trackId: string) => Promise<PlaylistDetail>
  removePlaylistTrack: (playlistId: string, trackId: string) => Promise<PlaylistDetail>
}

const PlaybackContext = createContext<PlaybackContextValue | null>(null)

export function PlaybackProvider({
  children,
  api,
}: {
  children: ReactNode
  api: PlaybackApi
}) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const queueRef = useRef<QueueItem[]>([])
  const currentTrackRef = useRef<Track | null>(null)
  const repeatModeRef = useRef<RepeatMode>('off')
  const apiRef = useRef(api)
  apiRef.current = api

  const [queue, setQueue] = useState<QueueItem[]>([])
  const [currentTrack, setCurrentTrack] = useState<Track | null>(null)
  const [isPlaying, setIsPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolumeState] = useState(0.8)
  const [shuffleEnabled, setShuffleEnabled] = useState(false)
  const [repeatMode, setRepeatMode] = useState<RepeatMode>('off')

  queueRef.current = queue
  currentTrackRef.current = currentTrack
  repeatModeRef.current = repeatMode

  const refreshQueue = useCallback(async () => {
    const data = await apiRef.current.getQueue()
    setQueue(data.items)
  }, [])

  const playTrackInternal = useCallback(
    async (trackId: string, queueOverride?: QueueItem[]) => {
      const items = queueOverride ?? queueRef.current
      const item = items.find(
        (entry) => entry.track.id === trackId || entry.trackId === trackId,
      )
      if (item) {
        setCurrentTrack(item.track)
        setDuration(
          item.track.durationMs > 0 ? item.track.durationMs / 1000 : 0,
        )
      }

      const audio = audioRef.current
      if (!audio) return

      audio.src = apiRef.current.getStreamUrl(trackId)
      setCurrentTime(0)
      try {
        await audio.play()
        setIsPlaying(true)
      } catch {
        setIsPlaying(false)
      }
    },
    [],
  )

  useEffect(() => {
    const audio = new Audio()
    audio.volume = volume
    audioRef.current = audio

    const onTimeUpdate = () => setCurrentTime(audio.currentTime)
    const onDurationChange = () => {
      const next = audio.duration
      if (Number.isFinite(next) && next > 0) {
        setDuration(next)
      }
    }
    const onLoadedMetadata = () => onDurationChange()
    const onPlay = () => setIsPlaying(true)
    const onPause = () => setIsPlaying(false)
    const onEnded = () => {
      setIsPlaying(false)
      const mode = repeatModeRef.current
      if (mode === 'once' || mode === 'loop') {
        audio.currentTime = 0
        setCurrentTime(0)
        if (mode === 'once') {
          repeatModeRef.current = 'off'
          setRepeatMode('off')
        }
        void audio.play()
        return
      }

      const items = queueRef.current
      const playing = currentTrackRef.current
      const idx = items.findIndex((item) => item.track.id === playing?.id)
      if (idx >= 0 && idx < items.length - 1) {
        void playTrackInternal(items[idx + 1].track.id)
      }
    }

    audio.addEventListener('timeupdate', onTimeUpdate)
    audio.addEventListener('durationchange', onDurationChange)
    audio.addEventListener('loadedmetadata', onLoadedMetadata)
    audio.addEventListener('ended', onEnded)
    audio.addEventListener('play', onPlay)
    audio.addEventListener('pause', onPause)

    void refreshQueue()

    return () => {
      audio.pause()
      audio.removeEventListener('timeupdate', onTimeUpdate)
      audio.removeEventListener('durationchange', onDurationChange)
      audio.removeEventListener('loadedmetadata', onLoadedMetadata)
      audio.removeEventListener('ended', onEnded)
      audio.removeEventListener('play', onPlay)
      audio.removeEventListener('pause', onPause)
      audioRef.current = null
    }
  }, [playTrackInternal, refreshQueue])

  const playTrack = useCallback(
    async (trackId: string, queueTrackIds?: string[]) => {
      let nextQueue = queueRef.current
      if (queueTrackIds) {
        const data = await apiRef.current.replaceQueue(queueTrackIds)
        nextQueue = data.items
        setQueue(data.items)
      }
      const trackInQueue = nextQueue.some((item) => item.track.id === trackId)
      if (!trackInQueue) {
        const data = await apiRef.current.appendQueueItem(trackId)
        nextQueue = data.items
        setQueue(data.items)
      }
      await playTrackInternal(trackId, nextQueue)
    },
    [playTrackInternal],
  )

  const queueTracks = useCallback(async (trackIds: string[]) => {
    let nextQueue = queueRef.current
    for (const trackId of trackIds) {
      const data = await apiRef.current.appendQueueItem(trackId)
      nextQueue = data.items
    }
    setQueue(nextQueue)
  }, [])

  const playQueueIndex = useCallback(
    async (index: number) => {
      const item = queueRef.current[index]
      if (!item) return
      await playTrackInternal(item.track.id)
    },
    [playTrackInternal],
  )

  const playNext = useCallback(async (trackId: string) => {
    const items = queueRef.current
    const playing = currentTrackRef.current
    const trackIds = items
      .map((item) => item.track.id)
      .filter((id) => id !== trackId)
    const currentIndex = playing
      ? items.findIndex((item) => item.track.id === playing.id)
      : -1
    const insertAt = currentIndex >= 0 ? currentIndex + 1 : trackIds.length
    trackIds.splice(insertAt, 0, trackId)
    const data = await apiRef.current.replaceQueue(trackIds)
    setQueue(data.items)
  }, [])

  const togglePlay = useCallback(() => {
    const audio = audioRef.current
    if (!audio) return
    if (audio.paused) {
      void audio.play()
    } else {
      audio.pause()
    }
  }, [])

  const toggleShuffle = useCallback(() => {
    setShuffleEnabled((enabled) => !enabled)
  }, [])

  const cycleRepeatMode = useCallback(() => {
    setRepeatMode((mode) => {
      if (mode === 'off') return 'once'
      if (mode === 'once') return 'loop'
      return 'off'
    })
  }, [])

  const seek = useCallback((seconds: number) => {
    const audio = audioRef.current
    if (!audio) return
    audio.currentTime = seconds
    setCurrentTime(seconds)
  }, [])

  const setVolume = useCallback((value: number) => {
    const clamped = Math.min(1, Math.max(0, value))
    setVolumeState(clamped)
    if (audioRef.current) {
      audioRef.current.volume = clamped
    }
  }, [])

  const removeFromQueue = useCallback(async (itemId: string) => {
    const data = await apiRef.current.removeQueueItem(itemId)
    setQueue(data.items)
  }, [])

  const clearQueue = useCallback(async () => {
    const data = await apiRef.current.replaceQueue([])
    setQueue(data.items)
    setCurrentTrack(null)
    setIsPlaying(false)
    if (audioRef.current) {
      audioRef.current.pause()
      audioRef.current.removeAttribute('src')
    }
  }, [])

  const value = useMemo(
    () => ({
      queue,
      currentTrack,
      isPlaying,
      currentTime,
      duration,
      volume,
      shuffleEnabled,
      repeatMode,
      playTrack,
      queueTracks,
      playQueueIndex,
      playNext,
      togglePlay,
      toggleShuffle,
      cycleRepeatMode,
      seek,
      setVolume,
      removeFromQueue,
      clearQueue,
      refreshQueue,
      getAlbumCoverUrl: (albumId: string) => apiRef.current.getAlbumCoverUrl(albumId),
      listPlaylists: () => apiRef.current.listPlaylists(),
      getPlaylist: (playlistId: string) => apiRef.current.getPlaylist(playlistId),
      createPlaylist: (name: string) => apiRef.current.createPlaylist(name),
      addPlaylistTrack: (playlistId: string, trackId: string) =>
        apiRef.current.addPlaylistTrack(playlistId, trackId),
      removePlaylistTrack: (playlistId: string, trackId: string) =>
        apiRef.current.removePlaylistTrack(playlistId, trackId),
    }),
    [
      queue,
      currentTrack,
      isPlaying,
      currentTime,
      duration,
      volume,
      shuffleEnabled,
      repeatMode,
      playTrack,
      queueTracks,
      playQueueIndex,
      playNext,
      togglePlay,
      toggleShuffle,
      cycleRepeatMode,
      seek,
      setVolume,
      removeFromQueue,
      clearQueue,
      refreshQueue,
    ],
  )

  return (
    <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>
  )
}

export function usePlayback() {
  const ctx = useContext(PlaybackContext)
  if (!ctx) {
    throw new Error('usePlayback must be used within PlaybackProvider')
  }
  return ctx
}
