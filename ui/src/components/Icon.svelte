<script lang="ts">
	// A few line icons, drawn on a 24 px grid with the current colour.
	let { name, size = 18, label = '', filled = false }: { name: string; size?: number; label?: string; filled?: boolean } = $props();

	const paths: Record<string, string> = {
		home: 'M3 11.5 12 4l9 7.5M5.5 9.5V20h13V9.5',
		tasks: 'M4 6h10M4 12h16M4 18h7M18 4v4M16 6h4',
		settings: 'M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Zm8-3.5-.1-1.3 2-1.6-2-3.4-2.4.9a8 8 0 0 0-2.2-1.3L14.9 3h-3.8l-.4 2.4a8 8 0 0 0-2.2 1.3L6.1 5.8l-2 3.4 2 1.6a8 8 0 0 0 0 2.5l-2 1.6 2 3.4 2.4-.9a8 8 0 0 0 2.2 1.3l.4 2.3h3.8l.4-2.3a8 8 0 0 0 2.2-1.3l2.4.9 2-3.4-2-1.6c.1-.4.1-.8.1-1.3Z',
		search: 'M10.5 18a7.5 7.5 0 1 1 0-15 7.5 7.5 0 0 1 0 15ZM21 21l-5.2-5.2',
		menu: 'M4 7h16M4 12h16M4 17h16',
		close: 'M6 6l12 12M18 6 6 18',
		folder: 'M3.5 6.5a2 2 0 0 1 2-2h4l2 2.5h7a2 2 0 0 1 2 2V17.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2v-11Z',
		refresh: 'M20 12a8 8 0 1 1-2.4-5.7M20 4v5h-5',
		download: 'M12 4v11m0 0-4.5-4.5M12 15l4.5-4.5M5 20h14',
		upload: 'M12 20V9m0 0-4.5 4.5M12 9l4.5 4.5M5 4h14',
		check: 'M5 12.5 10 17.5 19.5 7',
		alert: 'M12 8v5m0 3.5v.01M10.3 3.9 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z',
		back: 'M15 5 8 12l7 7',
		chevron: 'm9 5 7 7-7 7',
		up: 'm6 15 6-6 6 6',
		down: 'm6 9 6 6 6-6',
		trash: 'M4 7h16M9 7V4.5h6V7m-8.5 0 1 12.5h9l1-12.5',
		user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm-7.5 8.5a7.5 7.5 0 0 1 15 0',
		logout: 'M14 4h4.5a1.5 1.5 0 0 1 1.5 1.5v13a1.5 1.5 0 0 1-1.5 1.5H14M10 16l-4-4 4-4M6 12h10',
		lock: 'M6.5 11V8a5.5 5.5 0 0 1 11 0v3M5 11h14v9.5H5Z',
		grid: 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
		list: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
		star: 'M12 3.8l2.5 5.1 5.6.8-4 4 .9 5.6L12 16.6l-5 2.7.9-5.6-4-4 5.6-.8L12 3.8Z',
		chart: 'M4 20h16M7 16v-5M12 16V6M17 16v-8',
		disk: 'M4 7c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3Zm0 0v10c0 1.7 3.6 3 8 3s8-1.3 8-3V7M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
		tidy: 'M4 6h16M4 12h10M4 18h6M15 15l2.5 2.5L22 13',
		undo: 'M9 14 4 9l5-5M4 9h11a5 5 0 0 1 0 10h-3',
		share: 'M12 15V4m0 0L8 8m4-4 4 4M5 13v6.5h14V13',
		trophy: 'M8 4h8v5a4 4 0 0 1-8 0V4Zm0 1.5H4.5V7A3 3 0 0 0 8 10m8-4.5h3.5V7A3 3 0 0 1 16 10m-4 3v4m-4 3.5h8M9 17h6'
	};
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill={filled ? 'currentColor' : 'none'}
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden={label ? undefined : 'true'}
	aria-label={label || undefined}
	role={label ? 'img' : undefined}
	class="shrink-0"
>
	<path d={paths[name] ?? ''} />
</svg>
