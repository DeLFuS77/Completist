<script lang="ts">
	// How complete something is; at 100% the bar turns trophy gold.
	let { value, total, color = 'var(--color-ink)', height = 8, label = '' }: { value: number; total: number; color?: string; height?: number; label?: string } = $props();
	const pct = $derived(total > 0 ? Math.min(100, (value / total) * 100) : 0);
	const done = $derived(total > 0 && value >= total);
</script>

<div
	class="w-full overflow-hidden rounded-full bg-line/70"
	style="height: {height}px"
	role="progressbar"
	aria-valuemin="0"
	aria-valuemax={total}
	aria-valuenow={value}
	aria-label={label || undefined}
>
	<div class="h-full rounded-full transition-[width] duration-700 ease-[var(--ease-out-soft)]" style="width: {pct}%; background: {done ? 'var(--color-gold)' : color}"></div>
</div>
