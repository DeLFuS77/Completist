<script lang="ts">
	import { get, type GameDetail } from '../lib/api';
	import { collection } from '../lib/state.svelte';
	import { coverUrl } from '../lib/covers';
	import { systemColor, systemTint } from '../lib/systems';
	import { t, n, bytes, date } from '../lib/i18n.svelte';
	import Cover from '../components/Cover.svelte';
	import Icon from '../components/Icon.svelte';

	let { id }: { id: number } = $props();
	let game = $state<GameDetail | null>(null);
	let error = $state('');

	$effect(() => {
		collection.version;
		const gid = id;
		get<GameDetail>(`/api/games/${gid}`)
			.then((g) => {
				if (gid === id) game = g;
			})
			.catch((e) => (error = t(e.message)));
	});

	const color = $derived(game ? systemColor(game.system.id) : '');
	const snap = $derived(game ? coverUrl(game.system.id, game.name, 'Named_Snaps') : '');
	const title = $derived(game ? coverUrl(game.system.id, game.name, 'Named_Titles') : '');
	let snapOk = $state(false);
	let titleOk = $state(false);
	$effect(() => {
		snap;
		snapOk = false;
		titleOk = false;
	});

	const flagName: Record<string, string> = {
		bios: 'BIOS', demo: 'Demo', beta: 'Beta', proto: 'Prototype', sample: 'Sample', unlicensed: 'Unlicensed', pirate: 'Pirate',
		hack: 'Hack', aftermarket: 'Homebrew', program: 'Program', alt: 'Alternative dump', baddump: 'Bad dump'
	};
	const statusName: Record<string, string> = { ok: 'Verified', bad: 'Damaged or modified', unknown: 'Not identified', nodat: 'No DAT', error: 'Cannot be read' };
</script>

{#if error}
	<p class="text-bad">{error}</p>
{:else if !game}
	<div class="h-96 animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else}
	<header class="relative -mx-4 -mt-4 overflow-hidden px-4 pb-10 pt-8 sm:-mx-8 sm:-mt-8 sm:px-8">
		<!-- a screenshot of the game, blurred behind the cover -->
		<div class="absolute inset-0" style="background: {systemTint(game.system.id, 26)}" aria-hidden="true">
			{#if snap}
				<img src={snap} alt="" class="h-full w-full scale-110 object-cover blur-2xl transition-opacity duration-500" class:opacity-0={!snapOk} class:opacity-40={snapOk} onload={() => (snapOk = true)} />
			{/if}
			<div class="absolute inset-0 bg-gradient-to-b from-night/30 via-night/70 to-night"></div>
		</div>
		<div class="relative">
			<a href="/systems/{game.system.id}" class="inline-flex items-center gap-1 text-sm text-mute hover:text-ink"><Icon name="back" size={16} />{game.system.short}</a>
			<div class="mt-6 flex flex-col gap-8 sm:flex-row sm:items-end">
				<div class="w-44 shrink-0 sm:w-56">
					<Cover system={game.system.id} libretro={game.system.libretro} name={game.name} title={game.title} owned={game.owned} size="large" />
				</div>
				<div class="min-w-0 pb-1">
					<h1 class="t-title">{game.title}</h1>
					<p class="mt-2 break-words text-mute">{game.name}</p>
					<div class="mt-5 flex flex-wrap gap-2 text-sm">
						{#if game.owned}
							<span class="chip !border-good/50 !text-good"><Icon name="check" size={15} />{t('In your collection')}</span>
						{:else}
							<span class="chip !border-warn/50 !text-warn">{t('Missing')}</span>
						{/if}
						{#each game.regions as r}<span class="chip">{t(r)}</span>{/each}
						{#if game.languages.length}<span class="chip">{game.languages.join(', ')}</span>{/if}
						{#if game.revision}<span class="chip">{t('Revision {n}', { n: game.revision })}</span>{/if}
						{#each game.flags as f}<span class="chip">{t(flagName[f] ?? f)}</span>{/each}
					</div>
				</div>
			</div>
		</div>
	</header>

	<div class="grid gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]">
		<div class="min-w-0 space-y-10">
			{#if snap || title}
				<section aria-labelledby="shots">
					<h2 id="shots" class="t-section">{t('Screenshots')}</h2>
					<div class="mt-4 grid grid-cols-2 gap-4">
						<img src={title} alt={t('Title screen')} class="aspect-[4/3] w-full rounded-xl bg-panel object-cover [image-rendering:pixelated]" class:hidden={!titleOk} onload={() => (titleOk = true)} loading="lazy" />
						<img src={snap} alt={t('Screenshot')} class="aspect-[4/3] w-full rounded-xl bg-panel object-cover [image-rendering:pixelated]" class:hidden={!snapOk} loading="lazy" />
					</div>
					{#if !titleOk && !snapOk}<p class="mt-2 text-sm text-mute">{t('No screenshots were found for this game.')}</p>{/if}
				</section>
			{/if}

			<section aria-labelledby="files">
				<h2 id="files" class="t-section">{t('Your files')}</h2>
				{#if game.files.length === 0}
					<p class="mt-3 text-mute">{t('No file of yours is this game. A file with another name but the same content would be found too.')}</p>
				{:else}
					<ul class="mt-4 space-y-3">
						{#each game.files as f (f.id)}
							<li class="panel p-4 text-sm">
								<div class="break-all font-semibold">{f.entry ? `${f.path} › ${f.entry}` : f.path}</div>
								<div class="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-mute">
									<span class="text-good">{t(statusName[f.status] ?? f.status)}</span>
									<span>{bytes(f.size)}</span>
									{#if f.altForm === 'headerless'}<span>{t('With a copier header')}</span>{/if}
									{#if f.altForm === 'byteswapped'}<span>{t('In another byte order')}</span>{/if}
									<span>{t('Changed {d}', { d: date(f.mtime * 1000) })}</span>
								</div>
								<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 font-mono text-xs text-faint">
									<dt>CRC32</dt><dd class="break-all">{f.crc}</dd>
									<dt>SHA-1</dt><dd class="break-all">{f.sha1}</dd>
								</dl>
							</li>
						{/each}
					</ul>
					{#if game.files.length > 1}<p class="mt-3 text-sm text-warn">{t('You have this game {n} times.', { n: n(game.files.length) })}</p>{/if}
				{/if}
			</section>

			<section aria-labelledby="dat">
				<h2 id="dat" class="t-section">{t('In the DAT')}</h2>
				<ul class="mt-4 space-y-2 text-sm">
					{#each game.roms as r}
						<li class="flex flex-wrap gap-x-4 gap-y-1 text-mute">
							<span class="min-w-0 flex-1 break-all text-ink">{r.name}</span>
							<span>{bytes(r.size)}</span>
							<span class="font-mono text-xs">{r.crc}</span>
						</li>
					{/each}
				</ul>
			</section>
		</div>

		<aside aria-labelledby="versions">
			<h2 id="versions" class="t-section">{t('Versions and regions')}</h2>
			<ul class="panel mt-4 divide-y divide-line/60 px-4">
				{#each game.versions as v (v.id)}
					<li>
						<a href="/games/{v.id}" class="flex items-start gap-3 py-3 text-sm" aria-current={v.id === game.id ? 'page' : undefined}>
							<span class="mt-1 h-2.5 w-2.5 shrink-0 rounded-full" style="background: {v.owned ? color : 'transparent'}; border: 1.5px solid {v.owned ? color : 'var(--color-faint)'}"></span>
							<span class="min-w-0 flex-1" class:font-semibold={v.id === game.id}>
								<span class="block break-words">{v.regions.length ? v.regions.map((r) => t(r)).join(', ') : v.name}</span>
								<span class="block break-words text-xs text-faint">{v.name}</span>
							</span>
							<span class="shrink-0 text-xs" class:text-good={v.owned} class:text-faint={!v.owned}>{v.owned ? t('You have it') : t('Missing')}</span>
						</a>
					</li>
				{/each}
			</ul>
		</aside>
	</div>
{/if}
