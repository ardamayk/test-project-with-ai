import type { QueueItem } from "@repo/api-client";
import {
	type ComponentType,
	createContext,
	type ReactElement,
	type ReactNode,
	useContext,
} from "react";

export type QueueRowMenuProps = {
	item: QueueItem;
	children: (trigger: ReactNode) => ReactElement;
};

const QueueNavigationContext = createContext<
	((href: string) => void) | undefined
>(undefined);

const QueueRowMenuContext =
	createContext<ComponentType<QueueRowMenuProps> | null>(null);

/** Lets the host reuse its context menus without coupling shared UI to the app. */
export function QueueRowMenuProvider({
	menu,
	children,
	onNavigate,
}: {
	menu: ComponentType<QueueRowMenuProps>;
	children: ReactNode;
	onNavigate?: (href: string) => void;
}) {
	return (
		<QueueNavigationContext.Provider value={onNavigate}>
			<QueueRowMenuContext.Provider value={menu}>
				{children}
			</QueueRowMenuContext.Provider>
		</QueueNavigationContext.Provider>
	);
}

export function useQueueRowMenu() {
	return useContext(QueueRowMenuContext);
}

export function useQueueNavigation() {
	return useContext(QueueNavigationContext);
}
