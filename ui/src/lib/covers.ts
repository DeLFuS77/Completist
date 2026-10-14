// Box art from libretro-thumbnails, named after the No-Intro name of the game. The browser
// loads them directly for now; the server will cache them in a later version.

const base = 'https://thumbnails.libretro.com';

/** libretro replaces these characters with an underscore in file names */
function safe(name: string): string {
	return name.replace(/[&*/:`<>?\\|"]/g, '_');
}

export function coverUrl(libretroSystem: string, gameName: string, kind: 'Named_Boxarts' | 'Named_Snaps' | 'Named_Titles' = 'Named_Boxarts'): string {
	if (!libretroSystem) return '';
	return `${base}/${encodeURIComponent(libretroSystem)}/${kind}/${encodeURIComponent(safe(gameName))}.png`;
}
