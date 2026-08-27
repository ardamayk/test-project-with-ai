import { useMutation } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { Button } from "#/components/ui/button";
import { useLibraryScanStatus } from "#/hooks/use-library-scan-sync";
import { apiClient } from "#/lib/api";
import { cn } from "#/lib/utils";

export function ScanLibraryButton({ className }: { className?: string }) {
	const scanStatus = useLibraryScanStatus();

	const triggerScan = useMutation({
		mutationFn: () => apiClient.triggerLibraryScan(),
	});

	const status = scanStatus.data?.status ?? "idle";
	const isRunning = status === "running" || triggerScan.isPending;

	return (
		<Button
			type="button"
			variant="outline"
			size="sm"
			className={cn("shrink-0", className)}
			disabled={isRunning}
			title={
				isRunning
					? `Scanning… ${scanStatus.data?.scanned ?? 0} files`
					: "Scan library for new music"
			}
			onClick={() => triggerScan.mutate()}
		>
			<RefreshCw className={cn("size-4", isRunning && "animate-spin")} />
			{isRunning ? "Scanning…" : "Scan library"}
		</Button>
	);
}
