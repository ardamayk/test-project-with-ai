import {
	Empty,
	EmptyDescription,
	EmptyHeader,
	EmptyTitle,
} from "#/components/ui/empty";

export function ComingSoonPage({ title = "Coming soon" }: { title?: string }) {
	return (
		<div className="flex flex-1 items-center justify-center p-12">
			<Empty>
				<EmptyHeader>
					<EmptyTitle>{title}</EmptyTitle>
					<EmptyDescription>
						This section is planned for a future release.
					</EmptyDescription>
				</EmptyHeader>
			</Empty>
		</div>
	);
}
