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

export type PlaybackApi = {
  getQueue: () => Promise<Queue>
  replaceQueue: (trackIds: string[]) => Promise<Queue>
  appendQueueItem: (trackId: string) => Promise<Queue>
  removeQueueItem: (itemId: string) => Promise<Queue>
  getStreamUrl: (trackId: string) => string
}

type PlaybackContextValue = {
  queue: QueueItem[]
  currentTrack: Track | null
  isPlaying: boolean
  currentTime: number
  duration: number
  volume: number
  playTrack: (trackId: string, queueTrackIds?: string[]) => Promise<void>
  playQueueIndex: (index: number) => Promise<void>
  togglePlay: () => void
  seek: (seconds: number) => void
  setVolume: (value: number) => void
  removeFromQueue: (itemId: string) => Promise<void>
  clearQueue: () => Promise<void>
  refreshQueue: () => Promise<void>
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
  const apiRef = useRef(api)
  apiRef.current = api

  const [queue, setQueue] = useState<QueueItem[]>([])
  const [currentTrack, setCurrentTrack] = useState<Track | null>(null)
  const [isPlaying, setIsPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolumeState] = useState(0.8)

  queueRef.current = queue
  currentTrackRef.current = currentTrack

  const refreshQueue = useCallback(async () => {
    const data = await apiRef.current.getQueue()
    setQueue(data.items)
  }, [])

  const playTrackInternal = useCallback(async (trackId: string) => {
    const item = queueRef.current.find(
      (entry) => entry.track.id === trackId || entry.trackId === trackId,
    )
    if (item) {
      setCurrentTrack(item.track)
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
  }, [])

  useEffect(() => {
    const audio = new Audio()
    audio.volume = volume
    audioRef.current = audio

    const onTimeUpdate = () => setCurrentTime(audio.currentTime)
    const onDurationChange = () => setDuration(audio.duration || 0)
    const onPlay = () => setIsPlaying(true)
    const onPause = () => setIsPlaying(false)
    const onEnded = () => {
      setIsPlaying(false)
      const items = queueRef.current
      const playing = currentTrackRef.current
      const idx = items.findIndex((item) => item.track.id === playing?.id)
      if (idx >= 0 && idx < items.length - 1) {
        void playTrackInternal(items[idx + 1].track.id)
      }
    }

    audio.addEventListener('timeupdate', onTimeUpdate)
    audio.addEventListener('durationchange', onDurationChange)
    audio.addEventListener('ended', onEnded)
    audio.addEventListener('play', onPlay)
    audio.addEventListener('pause', onPause)

    void refreshQueue()

    return () => {
      audio.pause()
      audio.removeEventListener('timeupdate', onTimeUpdate)
      audio.removeEventListener('durationchange', onDurationChange)
      audio.removeEventListener('ended', onEnded)
      audio.removeEventListener('play', onPlay)
      audio.removeEventListener('pause', onPause)
      audioRef.current = null
    }
  }, [playTrackInternal, refreshQueue, volume])

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
        setQueue(data.items)
      }
      await playTrackInternal(trackId)
    },
    [playTrackInternal],
  )

  const playQueueIndex = useCallback(
    async (index: number) => {
      const item = queueRef.current[index]
      if (!item) return
      await playTrackInternal(item.track.id)
    },
    [playTrackInternal],
  )

  const togglePlay = useCallback(() => {
    const audio = audioRef.current
    if (!audio) return
    if (audio.paused) {
      void audio.play()
    } else {
      audio.pause()
    }
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
      playTrack,
      playQueueIndex,
      togglePlay,
      seek,
      setVolume,
      removeFromQueue,
      clearQueue,
      refreshQueue,
    }),
    [
      queue,
      currentTrack,
      isPlaying,
      currentTime,
      duration,
      volume,
      playTrack,
      playQueueIndex,
      togglePlay,
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
