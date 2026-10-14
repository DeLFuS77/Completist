<script lang="ts">
	import { coverUrl } from '../lib/covers';
	import { systemColor } from '../lib/systems';
	import { t } from '../lib/i18n.svelte';

	let {
		system,
		libretro,
		name,
		title,
		owned = true,
		size = 'grid'
	}: { system: string; libretro: string; name: string; title: string; owned?: boolean; size?: 'grid' | 'large' } = $props();

	const src = $derived(coverUrl(libretro ? system : '', name));
	// remembered by address, so the same component showing another game starts afresh
	let loadedSrc = $state('');
	let failedSrc = $state('');
	const loaded = $derived(loadedSrc === src);
	const failed = $derived(failedSrc === src);
	const color = $derived(systemColor(system));
</script>

<figure
	class="cover relative aspect-[3/4] w-full overflow-hidden rounded-[var(--radius-cover)]"
	class:missing={!owned}
	class:large={size === 'large'}
	style="--sys: {color}"
>
	<!-- the generated cover is under the image, so it shows while the image loads or when there is none -->
	<div class="generated absolute inset-0 flex flex-col justify-end p-3" class:invisible={loaded && !failed} aria-hidden={loaded && !failed}>
		<span class="font-display font-semibold leading-tight text-ink" class:text-sm={size === 'grid'} class:text-2xl={size === 'large'}>{title}</span>
	</div>
	{#if src && !failed}
		<img
			{src}
			alt={title}
			loading="lazy"
			decoding="async"
			class="absolute inset-0 h-full w-full object-contain transition-opacity duration-300"
			class:opacity-0={!loaded}
			onload={() => (loadedSrc = src)}
			onerror={() => (failedSrc = src)}
		/>
	{/if}
	{#if !owned}
		<figcaption class="absolute inset-x-0 bottom-0 bg-night/80 px-2 py-1.5 text-center text-xs font-semibold text-mute backdrop-blur-sm">
			{t('Missing')}
		</figcaption>
	{/if}
</figure>

<style>
	.cover {
		background: var(--color-panel);
		box-shadow: 0 1px 0 rgb(255 255 255 / 0.04) inset;
	}
	.generated {
		background:
			linear-gradient(160deg, color-mix(in oklab, var(--sys) 55%, var(--color-night)) 0%, color-mix(in oklab, var(--sys) 18%, var(--color-night)) 70%),
			var(--color-panel);
	}
	.generated::before {
		/* a cartridge label stripe, so a generated cover reads as a box and not as an error */
		content: '';
		position: absolute;
		inset: 10% 12% auto;
		height: 4px;
		border-radius: 2px;
		background: color-mix(in oklab, var(--sys) 80%, white);
		opacity: 0.55;
	}
	.missing {
		background: transparent;
		outline: 1.5px dashed var(--color-line);
		outline-offset: -1.5px;
	}
	.missing img,
	.missing .generated {
		filter: grayscale(1);
		opacity: 0.32;
	}
	.large {
		border-radius: 10px;
		box-shadow: 0 30px 60px -20px rgb(0 0 0 / 0.7);
	}
</style>
