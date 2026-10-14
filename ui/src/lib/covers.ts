// Box art, screenshots and title screens. The server downloads each image once from
// libretro-thumbnails and keeps it in its data folder.

const kinds = { Named_Boxarts: 'boxart', Named_Snaps: 'snap', Named_Titles: 'title' } as const;

export function coverUrl(system: string, gameName: string, kind: keyof typeof kinds = 'Named_Boxarts'): string {
	if (!system || !gameName) return '';
	return `/api/covers/${encodeURIComponent(system)}/${kinds[kind]}/${encodeURIComponent(gameName)}.png`;
}
