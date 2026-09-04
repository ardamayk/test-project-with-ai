import type { ComponentProps, ReactNode } from "react";
import { PAGE_CONTENT_WIDTH_CLASS } from "#/components/page-layout";
import { Button } from "#/components/ui/button";
import {
	Empty,
	EmptyContent,
	EmptyDescription,
	EmptyHeader,
	EmptyMedia,
	EmptyTitle,
} from "#/components/ui/empty";
import { cn } from "#/lib/utils";

const SKELETON_CARD_KEYS = [
	"skeleton-1",
	"skeleton-2",
	"skeleton-3",
	"skeleton-4",
	"skeleton-5",
	"skeleton-6",
	"skeleton-7",
	"skeleton-8",
	"skeleton-9",
	"skeleton-10",
] as const;
export const COLLECTION_PAGE_CONTAINER_CLASS = PAGE_CONTENT_WIDTH_CLASS;

export function CollectionPageContainer({
	className,
	...props
}: ComponentProps<"div">) {
	return (
		<div
			className={cn(COLLECTION_PAGE_CONTAINER_CLASS, className)}
			{...props}
		/>
	);
}

export function CollectionGrid({
	children,
	isBusy = false,
	className,
}: {
	children: ReactNode;
	isBusy?: boolean;
	className?: string;
}) {
	return (
		<div
			aria-busy={isBusy || undefined}
			className={cn(
				"grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-4 xl:grid-cols-5",
				className,
			)}
		>
			{children}
		</div>
	);
}

export function CollectionGridSkeleton({ label }: { label: string }) {
	return (
		<>
			<output className="sr-only" aria-live="polite">
				{label}
			</output>
			<CollectionGrid isBusy>
				{SKELETON_CARD_KEYS.map((key) => (
					<div
						key={key}
						data-testid="collection-card-skeleton"
						aria-hidden
						className="aspect-square animate-pulse rounded-md border border-border bg-muted/70 motion-reduce:animate-none"
					/>
				))}
			</CollectionGrid>
		</>
	);
}

export function CollectionGridState({
	kind,
	icon,
	title,
	description,
	onRetry,
	isRetrying = false,
}: {
	icon: ReactNode;
	title: string;
	description: string;
} & (
	| {
			kind: "empty";
			onRetry?: never;
			isRetrying?: never;
	  }
	| {
			kind: "error";
			onRetry: () => void;
			isRetrying?: boolean;
	  }
)) {
	const isError = kind === "error";

	return (
		<Empty
			role={isError ? "alert" : undefined}
			className={cn(
				"min-h-72 border",
				isError ? "border-destructive/30" : "border-border/70 bg-card/30",
			)}
		>
			<EmptyHeader>
				<EmptyMedia
					variant="icon"
					className={isError ? "text-destructive" : undefined}
				>
					{icon}
				</EmptyMedia>
				<EmptyTitle>{title}</EmptyTitle>
				<EmptyDescription>{description}</EmptyDescription>
			</EmptyHeader>
			{isError ? (
				<EmptyContent>
					<Button
						type="button"
						variant="outline"
						disabled={isRetrying}
						onClick={onRetry}
					>
						{isRetrying ? "Trying again…" : "Try again"}
					</Button>
				</EmptyContent>
			) : null}
		</Empty>
	);
}
