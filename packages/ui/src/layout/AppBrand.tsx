export function AppBrand({ compact = false }: { compact?: boolean }) {
	if (compact) {
		return (
			<div className="flex justify-center py-1" title="Earthly Audio">
				<div className="flex size-9 items-center justify-center rounded-lg bg-accent text-sidebar-heading">
					<span className="display-title font-semibold text-sm">E</span>
				</div>
			</div>
		);
	}

	return (
		<div className="min-w-0 flex-1">
			<p className="display-title font-semibold text-2xl text-[var(--shell-brand)] tracking-[-0.6px]">
				Earthly Audio
			</p>
			<p className="mt-1 font-medium text-sidebar-foreground text-xs tracking-[0.6px]">
				Premium Account
			</p>
		</div>
	);
}
