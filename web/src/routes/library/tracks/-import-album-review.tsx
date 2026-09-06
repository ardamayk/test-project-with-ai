import type {
	ManagedImportAlbumDecision,
	ManagedImportAlbumPreview,
} from "@repo/api-client";
import { useState } from "react";
import { apiClient } from "#/lib/api";
import { defaultAlbumDecision, importPositionKey } from "./-import-review";
import type { ImportFileEntry } from "./-managed-import-workflow";

type Props = {
	batchId: string;
	albums: ManagedImportAlbumPreview[];
	decisions: Record<string, ManagedImportAlbumDecision>;
	isBusy: boolean;
	onChange: (decision: ManagedImportAlbumDecision) => void;
	onUploaded: () => Promise<void>;
	onUploadStateChange: (isUploading: boolean) => void;
};

export function ImportAlbumReview(props: Props) {
	return (
		<div className="grid gap-3 px-4 py-3 sm:px-6">
			{props.albums.map((album) => (
				<AlbumReview key={album.key} {...props} album={album} />
			))}
		</div>
	);
}

function AlbumReview({
	album,
	...props
}: Props & { album: ManagedImportAlbumPreview }) {
	const decision = props.decisions[album.key] ?? defaultAlbumDecision(album);
	const existing = decision.createSeparate
		? undefined
		: album.existingAlbums.find(
				(candidate) => candidate.id === decision.albumId,
			);
	const [error, setError] = useState("");
	const [isUploading, setIsUploading] = useState(false);
	const isDisabled = props.isBusy || isUploading;
	async function upload(file: File | undefined) {
		if (!file) return;
		setIsUploading(true);
		props.onUploadStateChange(true);
		setError("");
		try {
			const artwork = await apiClient.uploadManagedImportArtwork(
				props.batchId,
				album.key,
				file,
			);
			await props.onUploaded();
			props.onChange({
				...decision,
				artworkMode: "selected",
				artworkId: artwork.id,
			});
		} catch (error) {
			setError(
				error instanceof Error ? error.message : "Album artwork upload failed",
			);
		} finally {
			setIsUploading(false);
			props.onUploadStateChange(false);
		}
	}
	return (
		<fieldset
			disabled={isDisabled}
			className="grid gap-3 rounded-md border border-border p-3"
		>
			<legend className="px-1 font-medium">
				{album.title} — {album.albumArtists.join(", ")}
			</legend>
			<label className="grid gap-1 text-sm">
				Album destination
				<select
					aria-label={`Album destination for ${album.title}`}
					className="rounded border border-border bg-background p-2"
					value={
						decision.createSeparate ? "separate" : (decision.albumId ?? "")
					}
					onChange={(event) =>
						props.onChange({
							...decision,
							createSeparate: event.target.value === "separate",
							albumId:
								event.target.value === "separate"
									? undefined
									: event.target.value,
							artworkMode:
								event.target.value === "separate" || !event.target.value
									? "auto"
									: "none",
							artworkId: undefined,
						})
					}
				>
					<option value="">
						{album.existingAlbums.length
							? "Choose an existing Album"
							: "Create new Album"}
					</option>
					{album.existingAlbums.map((match) => (
						<option key={match.id} value={match.id}>
							Add to existing Album{match.year ? ` (${match.year})` : ""} —{" "}
							{match.tracks.length} tracks
						</option>
					))}
					{album.existingAlbums.length > 0 ? (
						<option value="separate">Create separate Album</option>
					) : null}
				</select>
			</label>
			{existing?.hasArtwork ? (
				<p className="text-caption text-sm">
					Existing Album cover will be preserved.
				</p>
			) : (
				<>
					<label className="grid gap-1 text-sm">
						Album cover
						<select
							aria-label={`Album cover for ${album.title}`}
							className="rounded border border-border bg-background p-2"
							value={
								decision.artworkMode === "selected"
									? decision.artworkId
									: decision.artworkMode
							}
							onChange={(event) =>
								props.onChange({
									...decision,
									artworkMode:
										event.target.value === "auto" ||
										event.target.value === "none"
											? event.target.value
											: "selected",
									artworkId:
										event.target.value === "auto" ||
										event.target.value === "none"
											? undefined
											: event.target.value,
								})
							}
						>
							<option value="auto">
								{album.artworks.length > 1
									? "Choose one of the different covers"
									: "Use embedded cover when available"}
							</option>
							<option value="none">Continue without artwork</option>
							{album.artworks.map((artwork, index) => (
								<option key={artwork.id} value={artwork.id}>
									{artwork.jobId
										? `Embedded cover ${index + 1}`
										: "Uploaded cover"}
								</option>
							))}
						</select>
					</label>
					<div className="flex flex-wrap gap-2">
						{album.artworks.map((artwork, index) => (
							<button
								key={artwork.id}
								type="button"
								aria-label={`Select cover ${index + 1} for ${album.title}`}
								onClick={() =>
									props.onChange({
										...decision,
										artworkMode: "selected",
										artworkId: artwork.id,
									})
								}
								className="rounded border border-border p-1"
							>
								<img
									className="size-20 rounded object-cover"
									alt={`Cover ${index + 1}`}
									src={apiClient.getManagedImportArtworkUrl(
										props.batchId,
										artwork.id,
									)}
								/>
							</button>
						))}
					</div>
					<label className="text-sm">
						Upload JPG or PNG (maximum 20 MiB)
						<input
							aria-label={`Upload cover for ${album.title}`}
							className="mt-1 block w-full text-sm"
							type="file"
							accept=".jpg,.jpeg,.png,image/jpeg,image/png"
							onChange={(event) => {
								void upload(event.target.files?.[0]);
								event.target.value = "";
							}}
						/>
					</label>
					{existing ? (
						<p className="text-caption text-sm">
							Your import confirmation also approves adding this cover to the
							existing Album.
						</p>
					) : null}
				</>
			)}
			{isUploading ? <p aria-live="polite">Uploading artwork…</p> : null}
			{error ? (
				<p role="alert" className="text-destructive text-sm">
					{error}
				</p>
			) : null}
		</fieldset>
	);
}

export function ImportFileAlternatives({
	entries,
	isBusy,
	onSelect,
}: {
	entries: ImportFileEntry[];
	isBusy: boolean;
	onSelect: (key: string, selected: boolean) => void;
}) {
	const groups = new Map<string, ImportFileEntry[]>();
	for (const entry of entries) {
		if (entry.state !== "accepted" || !entry.preview) continue;
		const key = importPositionKey(entry);
		groups.set(key, [...(groups.get(key) ?? []), entry]);
	}
	return (
		<div className="grid gap-2 px-4 sm:px-6">
			{[...groups.entries()]
				.filter(
					([, files]) =>
						files.length > 1 &&
						new Set(
							files.map((file) => file.preview?.file.contentSha256 ?? file.key),
						).size > 1,
				)
				.map(([key, files]) => (
					<fieldset
						key={key}
						disabled={isBusy}
						className="rounded border border-border p-3"
					>
						<legend className="px-1 text-sm">
							Choose one file for {files[0].preview?.file.album}, position{" "}
							{files[0].preview?.file.discNo}.{files[0].preview?.file.trackNo}
						</legend>
						{files.map((entry) => (
							<label key={entry.key} className="flex gap-2 py-1 text-sm">
								<input
									type="radio"
									name={key}
									checked={
										entry.selected &&
										files.filter((file) => file.selected).length === 1
									}
									onChange={() => onSelect(entry.key, true)}
								/>
								{entry.file.name} — {entry.preview?.file.format.toUpperCase()},{" "}
								{((entry.preview?.file.sizeBytes ?? 0) / 1024 / 1024).toFixed(
									1,
								)}{" "}
								MiB, {entry.preview?.file.sampleRateHz} Hz,{" "}
								{entry.preview &&
								"bitDepth" in entry.preview.file &&
								entry.preview.file.bitDepth
									? `${entry.preview.file.bitDepth}-bit, `
									: ""}
								{entry.preview?.file.bitrateKbps} kbps
							</label>
						))}
					</fieldset>
				))}
		</div>
	);
}
