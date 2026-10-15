import { theme } from './theme.svelte';

// The colour of each system, taken from the hardware or its logo. It tints its cards, its
// generated covers and its band of the mosaic.

const colors: Record<string, string> = {
	nes: '#e4473c',
	fds: '#f2c14e',
	snes: '#ab9df4',
	n64: '#2fae66',
	virtualboy: '#e2363b',
	gb: '#9bbc0f',
	gbc: '#f05a9b',
	gba: '#4a4ed8',
	nds: '#9aa4b5',
	pokemini: '#f6c744',
	sg1000: '#c7cdd6',
	mastersystem: '#e5484d',
	megadrive: '#3d7bf7',
	sega32x: '#f2994a',
	gamegear: '#3bc9db',
	pcengine: '#f76707',
	atari2600: '#d9a066',
	atari7800: '#b07d4f',
	lynx: '#f5b041',
	ngp: '#9775fa',
	ngpc: '#845ef7',
	wonderswan: '#74c0fc',
	wonderswancolor: '#4dabf7',
	colecovision: '#adb5bd',
	intellivision: '#c9a86a',
	vectrex: '#63e6be',
	psx: '#5c7cfa',
	saturn: '#20c997',
	segacd: '#e64980',
	dreamcast: '#ff6b35',
	gamecube: '#7048e8',
	wii: '#e9ecef',
	ps2: '#1c7ed6',
	psp: '#a5d8ff'
};

// the colours too pale to see on the light theme, made darker there
const onLight: Record<string, string> = {
	wii: '#8e9ab0',
	psp: '#4a9fe0',
	sg1000: '#8d96a3',
	colecovision: '#868e96'
};

export function systemColor(id: string): string {
	return (theme.light && onLight[id]) || colors[id] || '#8c96b5';
}

/** the colour mixed into the background, for large surfaces */
export function systemTint(id: string, amount = 22): string {
	return `color-mix(in oklab, ${systemColor(id)} ${amount}%, var(--color-night))`;
}
