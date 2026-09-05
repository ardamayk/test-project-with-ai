import type { HealthResponse } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Switch } from "#/components/ui/switch";
import { apiClient } from "#/lib/api";

const STORAGE_KEY = "earthly-audio.import.recording-identification";

type Availability = {
	isAvailable: boolean;
	reason?: string;
};

const unavailableReasons: Record<
	Exclude<HealthResponse["recordingIdentification"]["status"], "enabled">,
	string
> = {
	disabled_by_config: "disabled on this Music Server",
	missing_fpcalc: "fpcalc is not installed on the Music Server",
	missing_api_key: "no AcoustID key on the Music Server",
};

/** Derives whether the Import Music switch may be turned on (ADR 0017). */
export function recordingIdentificationAvailability(
	health: HealthResponse | undefined,
): Availability {
	if (!health) return { isAvailable: false, reason: "checking the server…" };
	const status = health.recordingIdentification?.status;
	if (!status) {
		return {
			isAvailable: false,
			reason: "not supported by this Music Server",
		};
	}
	if (status === "enabled") return { isAvailable: true };
	return { isAvailable: false, reason: unavailableReasons[status] };
}

/** Remembers the user's last choice in this browser; on by default. */
export function readStoredRecordingIdentification(): boolean {
	try {
		return localStorage.getItem(STORAGE_KEY) !== "off";
	} catch {
		return true;
	}
}

function storeRecordingIdentification(isOn: boolean) {
	try {
		localStorage.setItem(STORAGE_KEY, isOn ? "on" : "off");
	} catch {
		// Storage can be unavailable (private mode); the switch still works.
	}
}

/**
 * Per-batch switch shown in the Import Music dialog. It reports its
 * effective value through onChange so the workflow can create the batch
 * with it; unavailable identification always reports false.
 */
export function RecordingIdentificationSwitch({
	isDisabled,
	onChange,
}: {
	isDisabled: boolean;
	onChange: (isOn: boolean) => void;
}) {
	const health = useQuery({
		queryKey: ["health"],
		queryFn: () => apiClient.getHealth(),
		staleTime: Number.POSITIVE_INFINITY,
	});
	const availability = recordingIdentificationAvailability(health.data);
	const [isOn, setIsOn] = useState(readStoredRecordingIdentification);
	const effective = availability.isAvailable && isOn;

	useEffect(() => {
		onChange(effective);
	}, [effective, onChange]);

	return (
		<div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 px-3 py-2.5">
			<span className="grid gap-0.5">
				<span
					id="recording-identification-label"
					className="font-medium text-heading text-sm"
				>
					Identify recordings with MusicBrainz
				</span>
				<span className="text-caption text-xs">
					{availability.isAvailable
						? "Fingerprints each file and corrects title and artists from MusicBrainz; tags stay as the fallback."
						: `Unavailable: ${availability.reason}`}
				</span>
			</span>
			<Switch
				aria-labelledby="recording-identification-label"
				checked={effective}
				disabled={isDisabled || !availability.isAvailable}
				onCheckedChange={(checked) => {
					setIsOn(checked);
					storeRecordingIdentification(checked);
				}}
			/>
		</div>
	);
}
