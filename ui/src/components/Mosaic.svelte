<script lang="ts">
	// One tile per game to collect, every system in a row: the ones you have lit in the colour of
	// their system, the missing ones dim. On first show the owned tiles light up in one sweep.
	import type { MosaicRow } from '../lib/api';
	import { systemColor } from '../lib/systems';
	import { t, n } from '../lib/i18n.svelte';

	let { rows, names }: { rows: MosaicRow[]; names: Record<string, string> } = $props();

	let canvas: HTMLCanvasElement;
	let wrap: HTMLDivElement;
	let width = $state(0);
	let hover = $state<{ x: number; y: number; id: string; have: number; total: number } | null>(null);

	type Tile = { system: string; owned: boolean };
	const tiles = $derived.by(() => {
		const out: Tile[] = [];
		for (const row of rows) {
			const bits = Uint8Array.from(atob(row.bits), (c) => c.charCodeAt(0));
			for (let i = 0; i < row.total; i++) out.push({ system: row.id, owned: (bits[i >> 3] & (1 << (i & 7))) !== 0 });
		}
		return out;
	});
	const counts = $derived(
		Object.fromEntries(
			rows.map((r) => {
				const bits = Uint8Array.from(atob(r.bits), (c) => c.charCodeAt(0));
				let have = 0;
				for (let i = 0; i < r.total; i++) if (bits[i >> 3] & (1 << (i & 7))) have++;
				return [r.id, { have, total: r.total }];
			})
		)
	);

	// tiles as large as fit in about 300 px of height, between 3 and 11 px
	const layout = $derived.by(() => {
		const total = Math.max(1, tiles.length);
		const target = 300;
		let size = Math.floor(Math.sqrt((width * target) / total));
		size = Math.max(3, Math.min(11, size));
		const gap = size >= 6 ? 2 : 1;
		const step = size + gap;
		const perRow = Math.max(1, Math.floor((width + gap) / step));
		const height = Math.ceil(total / perRow) * step - gap;
		return { size, gap, step, perRow, height: Math.max(height, 0) };
	});

	let progress = 0; // 0..1 of the sweep
	let raf = 0;
	let swept = false;

	function draw() {
		if (!canvas || width === 0) return;
		const dpr = window.devicePixelRatio || 1;
		const { size, step, perRow, height } = layout;
		canvas.width = Math.round(width * dpr);
		canvas.height = Math.round(height * dpr);
		canvas.style.height = `${height}px`;
		const ctx = canvas.getContext('2d')!;
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
		ctx.clearRect(0, 0, width, height);
		const lit = Math.floor(progress * tiles.length);
		let last = '';
		for (let i = 0; i < tiles.length; i++) {
			const tile = tiles[i];
			const x = (i % perRow) * step;
			const y = Math.floor(i / perRow) * step;
			const on = tile.owned && i <= lit;
			if (tile.system !== last) {
				ctx.fillStyle = systemColor(tile.system);
				last = tile.system;
			}
			ctx.globalAlpha = on ? 1 : 0.2;
			const r = size >= 6 ? 1.5 : 0;
			if (r) {
				ctx.beginPath();
				ctx.roundRect(x, y, size, size, r);
				ctx.fill();
			} else ctx.fillRect(x, y, size, size);
		}
		ctx.globalAlpha = 1;
	}

	function sweep() {
		const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
		if (reduce || swept) {
			progress = 1;
			draw();
			return;
		}
		swept = true;
		const start = performance.now();
		const duration = 1100;
		const frame = (now: number) => {
			const p = Math.min(1, (now - start) / duration);
			progress = 1 - Math.pow(1 - p, 3);
			draw();
			if (p < 1) raf = requestAnimationFrame(frame);
		};
		raf = requestAnimationFrame(frame);
	}

	$effect(() => {
		const ro = new ResizeObserver(([e]) => (width = Math.floor(e.contentRect.width)));
		ro.observe(wrap);
		return () => {
			ro.disconnect();
			cancelAnimationFrame(raf);
		};
	});

	$effect(() => {
		// redraw when the data or the size changes; the first time with the sweep
		tiles;
		layout;
		if (width > 0) {
			if (!swept) sweep();
			else {
				progress = 1;
				draw();
			}
		}
	});

	function move(e: PointerEvent) {
		const rect = canvas.getBoundingClientRect();
		const x = e.clientX - rect.left;
		const y = e.clientY - rect.top;
		const col = Math.floor(x / layout.step);
		const row = Math.floor(y / layout.step);
		const i = row * layout.perRow + col;
		const tile = tiles[i];
		if (!tile || col >= layout.perRow) {
			hover = null;
			return;
		}
		const c = counts[tile.system];
		hover = { x, y, id: tile.system, have: c.have, total: c.total };
	}
</script>

<div class="relative" bind:this={wrap}>
	<canvas
		bind:this={canvas}
		class="block w-full"
		role="img"
		aria-label={t('One tile per game of your sets; lit tiles are the games you have.')}
		onpointermove={move}
		onpointerleave={() => (hover = null)}
	></canvas>
	{#if hover}
		<div
			class="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-lg bg-raised px-3 py-2 text-sm shadow-xl"
			style="left: {Math.min(Math.max(hover.x, 90), width - 90)}px; top: {hover.y - 8}px"
		>
			<span class="font-semibold" style="color: {systemColor(hover.id)}">{names[hover.id] ?? hover.id}</span>
			<span class="text-mute"> {n(hover.have)} / {n(hover.total)}</span>
		</div>
	{/if}
</div>
