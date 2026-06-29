import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '#/components/ui/button'
import { apiClient } from '#/lib/api'

export function ScanBanner() {
  const queryClient = useQueryClient()
  const scanStatus = useQuery({
    queryKey: ['library', 'scan-status'],
    queryFn: () => apiClient.getLibraryScanStatus(),
    refetchInterval: (query) =>
      query.state.data?.status === 'running' ? 2000 : false,
  })

  const triggerScan = useMutation({
    mutationFn: () => apiClient.triggerLibraryScan(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  const status = scanStatus.data?.status ?? 'idle'
  const isRunning = status === 'running'

  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/30 px-4 py-3">
      <div className="text-sm">
        <p className="font-medium">Library scan</p>
        <p className="text-muted-foreground text-xs">
          {isRunning
            ? `Scanning… ${scanStatus.data?.scanned ?? 0} files processed`
            : status === 'completed'
              ? `Last scan: +${scanStatus.data?.added ?? 0} added, ${scanStatus.data?.updated ?? 0} updated, ${scanStatus.data?.removed ?? 0} removed`
              : status === 'failed'
                ? `Scan failed: ${scanStatus.data?.error ?? 'unknown error'}`
                : 'Put audio files in a folder (e.g. ./music), set MUSIC_PATHS in server/.env, then scan'}
        </p>
      </div>
      <Button
        type="button"
        size="sm"
        disabled={isRunning || triggerScan.isPending}
        onClick={() => triggerScan.mutate()}
      >
        {isRunning ? 'Scanning…' : 'Scan library'}
      </Button>
    </div>
  )
}
