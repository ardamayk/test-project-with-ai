import { PageHeader, PageShell } from "#/components/page-layout";
import { ScanLibraryButton } from "#/components/scan-library-button";

export function HomePage() {
	return (
		<PageShell
			testId="home-page-shell"
			contentTestId="home-page-content"
			header={
				<PageHeader
					title="Home"
					description="Library actions and overview will live here."
					actions={<ScanLibraryButton />}
				/>
			}
		>
			<div className="rounded-lg border border-dashed border-border p-6 text-caption text-sm">
				Home layout will be designed later.
			</div>
		</PageShell>
	);
}
