import { useQueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useState } from "react";
import { useReturnFocus } from "#/hooks/use-return-focus";
import { ImportMusicDialog } from "#/routes/library/tracks/-import-music-dialog";

const ImportSessionContext = createContext<{ open: () => void } | null>(null);

export function useManagedImport() {
	const session = useContext(ImportSessionContext);
	if (!session)
		throw new Error("ImportSessionProvider is required for Managed Import");
	return session;
}

export function ImportSessionProvider({ children }: { children: ReactNode }) {
	const session = useImportSession();
	return (
		<ImportSessionContext.Provider value={session}>
			{children}
			<ImportMusicDialog
				isOpen={session.isOpen}
				onOpenChange={session.handleOpenChange}
				onCommitted={session.refresh}
				onCloseAutoFocus={session.restoreFocus}
			/>
		</ImportSessionContext.Provider>
	);
}

function useImportSession() {
	const [isOpen, setIsOpen] = useState(false);
	const queryClient = useQueryClient();
	// The dialog opens from the plus action without a DialogTrigger, so remember
	// the opener to restore focus on close.
	const returnFocus = useReturnFocus();
	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: ["library", "tracks"] });
	}
	function handleOpenChange(nextIsOpen: boolean) {
		setIsOpen(nextIsOpen);
	}
	return {
		isOpen,
		open: () => {
			returnFocus.capture();
			setIsOpen(true);
		},
		handleOpenChange,
		refresh,
		restoreFocus: returnFocus.restore,
	};
}
