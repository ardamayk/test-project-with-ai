import type { ReactNode } from "react";
import { cn } from "#/lib/utils";

export const HEADER_SEARCH_CONTAINER_CLASS = "relative w-full sm:w-[28rem]";
export const HEADER_SEARCH_INPUT_CLASS =
	"h-11 rounded-xl bg-[var(--player)] pl-10 text-sm";

export function PageShell({
	testId,
	header,
	children,
	className,
	contentClassName,
	contentTestId,
}: {
	testId?: string;
	header: ReactNode;
	children: ReactNode;
	className?: string;
	contentClassName?: string;
	contentTestId?: string;
}) {
	return (
		<div
			data-testid={testId}
			className={cn("flex min-h-0 flex-1 flex-col overflow-hidden", className)}
		>
			{header}
			<div
				data-testid={contentTestId}
				className={cn(
					"min-h-0 flex-1 overflow-y-auto px-6 py-5 [scrollbar-width:none] md:px-8 [&::-webkit-scrollbar]:hidden",
					contentClassName,
				)}
			>
				{children}
			</div>
		</div>
	);
}

export function PageHeader({
	title,
	description,
	actions,
	footer,
}: {
	title: string;
	description?: string;
	actions?: ReactNode;
	footer?: ReactNode;
}) {
	return (
		<header className="sticky top-0 z-40 shrink-0 border-border border-b bg-background/80 px-6 py-3 backdrop-blur md:px-8">
			<div className="flex flex-col gap-2">
				<div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
					<div className="min-w-0">
						<h1 className="font-semibold text-2xl text-heading tracking-normal">
							{title}
						</h1>
						{description ? (
							<p className="mt-1.5 max-w-2xl text-foreground text-xs">
								{description}
							</p>
						) : null}
					</div>
					{actions ? (
						<div className="flex min-w-0 items-center gap-2 xl:justify-end">
							{actions}
						</div>
					) : null}
				</div>
				{footer}
			</div>
		</header>
	);
}
