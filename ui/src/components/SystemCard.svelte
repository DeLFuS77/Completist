<script lang="ts">
	import type { Progress } from '../lib/api';
	import { systemColor, systemTint } from '../lib/systems';
	import { t, n } from '../lib/i18n.svelte';
	import Bar from './Bar.svelte';
	import Icon from './Icon.svelte';

	let { p }: { p: Progress } = $props();
	const color = $derived(systemColor(p.system.id));
	const complete = $derived(p.hasDat && p.total > 0 && p.missing === 0);
</script>

<a
	href="/systems/{p.system.id}"
	class="card group relative flex flex-col gap-4 overflow-hidden rounded-2xl p-5 transition-colors"
	style="--sys: {color}; background: {systemTint(p.system.id, 14)}"
>
	<div class="flex items-start justify-between gap-3">
		<div>
			<div class="text-sm text-mute">{p.system.maker}</div>
			<h3 class="font-display text-lg font-semibold leading-tight tracking-tight">{p.system.short}</h3>
		</div>
		{#if complete}
			<span class="text-gold" title={t('Complete set')}><Icon name="trophy" size={22} label={t('Complete set')} /></span>
		{/if}
	</div>

	{#if p.hasDat}
		<div class="mt-auto">
			<div class="flex items-baseline justify-between gap-2">
				<span class="t-num text-2xl font-semibold">{n(Math.floor(p.percent))}<span class="text-base text-mute">%</span></span>
				<span class="text-sm text-mute"><span class="t-num text-ink">{n(p.have)}</span> / {n(p.total)}</span>
			</div>
			<div class="mt-2"><Bar value={p.have} total={p.total} {color} label={p.system.short} /></div>
			<div class="mt-2 text-sm" class:text-mute={!complete} class:text-gold={complete}>
				{complete ? t('Nothing missing') : t('{n} missing', { n: n(p.missing) })}
			</div>
		</div>
	{:else}
		<div class="mt-auto text-sm text-mute">
			{t('{n} files without a DAT', { n: n(p.files.files) })}
		</div>
	{/if}
	<span class="edge pointer-events-none absolute inset-y-0 left-0 w-1" aria-hidden="true"></span>
</a>

<style>
	.card:hover {
		background: color-mix(in oklab, var(--sys) 22%, var(--color-night)) !important;
	}
	.edge {
		background: var(--sys);
	}
</style>
