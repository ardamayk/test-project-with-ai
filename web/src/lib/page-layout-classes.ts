/**
 * Horizontal padding every page region shares so headers, list content and
 * detail content line up along the same left edge.
 */
export const PAGE_CONTENT_PADDING_CLASS = "px-6 py-5 md:px-8";

/**
 * Width the page content is centred at once the viewport grows past the widest
 * supported grid. Every page region that holds content applies this, so a list
 * page and the detail page it links to stay on the same vertical line.
 */
export const PAGE_CONTENT_WIDTH_CLASS =
	"w-full min-[1801px]:mx-auto min-[1801px]:max-w-[1476px]";

export const HEADER_SEARCH_CONTAINER_CLASS = "relative w-full sm:w-[28rem]";
export const HEADER_SEARCH_INPUT_CLASS =
	"h-11 rounded-xl bg-[var(--player)] pl-10 text-sm";
