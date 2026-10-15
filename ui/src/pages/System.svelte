<script lang="ts">
	import { get, type Entry, type GameList, type Progress, type Review } from '../lib/api';
	import { collection } from '../lib/state.svelte';
	import { router, setParam } from '../lib/router.svelte';
	import { systemColor, systemTint } from '../lib/systems';
	import { t, n, plural, bytes } from '../lib/i18n.svelte';
	import Cover from '../components/Cover.svelte';
	import Bar from '../components/Bar.svelte';
	import Icon from '../components/Icon.svelte';

	let { id }: { id: string } = $props();

	let progress = $state<Progress | null>(null);
	let list = $state<GameList | null>(null);
	let error = $state('');

	const filter = $derived(router.search.get('show') ?? 'all');
	const query = $derived(router.search.get('q') ?? '');
	const region = $derived(router.search.get('region') ?? '');
	const view = $derived(router.search.get('view') ?? 'grid');

	$effect(() => {
		// reload with the system and whenever the collection changes
		collection.version;
		const sys = id;
		error = '';
		Promise.all([get<Progress>(`/api/systems/${sys}`), get<GameList>(`/api/systems/${sys}/games`)])
			.then(([p, l]) => {
				if (sys !== id) return;
				progress = p;
				list = l;
			})
			.catch((e) => (error = t(e.message)));
	});

	const items = $derived.by(() => {
		if (!list) return [] as Entry[];
		const q = query.trim().toLowerCase();
		return list.items.filter(
			(e) =>
				(filter === 'all' || (filter === 'wanted' ? e.wanted : filter === 'have' ? e.owned : !e.owned)) &&
				(!q || e.name.toLowerCase().includes(q)) &&
				(!region || e.regions.includes(region))
		);
	});
	const wantedCount = $derived(list?.items.filter((e) => e.wanted).length ?? 0);
	const regions = $derived(Object.entries(list?.regions ?? {}).sort((a, b) => b[1] - a[1]).map(([r]) => r));

	// the grid grows as you scroll, so a system with thousands of games opens at once
	let shown = $state(120);
	$effect(() => {
		filter;
		query;
		region;
		id;
		shown = 120;
	});
	let sentinel = $state<HTMLElement>();
	$effect(() => {
		if (!sentinel) return;
		const io = new IntersectionObserver((e) => {
			if (e[0].isIntersecting) shown += 180;
		}, { rootMargin: '1200px' });
		io.observe(sentinel);
		return () => io.disconnect();
	});

	const color = $derived(systemColor(id));
	const modeName = (m: string) => (m === '1g1r' ? t('One version of each game') : m === 'official' ? t('Every official release') : t('Every entry of the DAT'));

	// files to review
	const fileStatus = $derived(router.search.get('files') ?? '');
	const reviewStatus = $derived(fileStatus || (progress && progress.files.bad === 0 ? 'unknown' : 'bad'));
	const reason = $derived(router.search.get('why') ?? '');
	let review = $state<Review | null>(null);
	$effect(() => {
		collection.version;
		const status = reviewStatus;
		const why = reason;
		get<Review>(`/api/systems/${id}/files?status=${status}${why ? `&reason=${why}` : ''}`)
			.then((r) => (review = r))
			.catch(() => (review = null));
	});
	const fromLibretro = $derived(review?.dat?.source === 'libretro-database');
	// why a file is to review, in the order they are shown, with what it means and what to do
	const reasonText = $derived<Record<string, { title: string; text: string }>>({
		size: {
			title: t('Another size than the DAT'),
			text: t('The name is right but the size is not. Most often it is another dump of the game, from a set older or newer than the DAT; it can also be a trimmed, padded or damaged file. If your set is from another date, a DAT of that date recognises it.')
		},
		content: {
			title: t('Same size, other content'),
			text: t('The name and the size are right, the content is not: another revision, a hack, a patched or damaged file, or a dump the DAT has since replaced.')
		},
		prerelease: {
			title: t('Betas, prototypes and demos'),
			text: fromLibretro
				? t('The DATs of libretro-database leave out betas, prototypes and demos, so they are not recognised. Import the official No-Intro or Redump DAT to recognise them.')
				: t('This DAT does not list them.')
		},
		homebrew: {
			title: t('Homebrew and unlicensed games'),
			text: fromLibretro
				? t('New games for old systems and unlicensed releases. libretro-database lists only part of them; the official No-Intro DAT has more.')
				: t('New games for old systems and unlicensed releases that this DAT does not list.')
		},
		pirate: { title: t('Pirate releases'), text: t('Bootleg cartridges. Few DATs list them all.') },
		hack: { title: t('Hacks, translations and tagged dumps'), text: t('Files changed by fans, or named with GoodTools tags such as [b] (bad dump) or [T+] (translation). No-Intro and Redump do not list them.') },
		program: { title: t('Test programs and tools'), text: t('Software that is not a game, such as test suites.') },
		nodat: { title: t('No DAT for this system'), text: t('Download or import a DAT in Settings > DATs to identify them.') },
		other: {
			title: t('Not in the DAT'),
			text: t('A release newer than the DAT, a bad dump or a file nobody verified. Updating the DATs, or importing the official one, may recognise it.')
		}
	});
	const reasonOrder = ['size', 'content', 'prerelease', 'homebrew', 'pirate', 'hack', 'program', 'nodat', 'other'];
	const shownReasons = $derived(reasonOrder.filter((r) => review?.reasons[r]));
	const reviewCounts = $derived(progress ? { bad: progress.files.bad, unknown: progress.files.unknown + progress.files.noDat } : { bad: 0, unknown: 0 });

	let searchValue = $state('');
	$effect(() => {
		searchValue = query;
	});
	let searchTimer: ReturnType<typeof setTimeout>;
	function onSearch(v: string) {
		searchValue = v;
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => setParam('q', v), 150);
	}
</script>

{#if error}
	<p class="text-bad">{error}</p>
{:else if !progress}
	<div class="h-64 animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else}
	<header class="relative -mx-4 -mt-4 overflow-hidden px-4 pb-8 pt-10 sm:-mx-8 sm:-mt-8 sm:px-8" style="background: linear-gradient(180deg, {systemTint(id, 30)} 0%, var(--color-night) 100%)">
		<div class="text-mute">{progress.system.maker}, {progress.system.year}</div>
		<h1 class="t-hero mt-1" style="color: color-mix(in oklab, {color} 35%, var(--color-ink))">{progress.system.short}</h1>
		{#if progress.hasDat}
			<div class="mt-6 max-w-3xl">
				<div class="flex flex-wrap items-baseline gap-x-6 gap-y-1">
					<span class="t-num text-3xl font-semibold">{n(progress.have)} <span class="text-mute">/ {n(progress.total)}</span></span>
					<span class="text-lg" class:text-gold={progress.missing === 0} class:text-mute={progress.missing > 0}>
						{progress.missing === 0 ? t('Nothing missing') : t('{n} missing', { n: n(progress.missing) })}
					</span>
					<a href="/settings/counting" class="text-sm text-mute underline decoration-line underline-offset-4 hover:text-ink">{modeName(progress.mode)}</a>
					{#if progress.missing > 0}
						<a href="/api/systems/{id}/fixdat" download class="inline-flex items-center gap-1 text-sm text-mute underline decoration-line underline-offset-4 hover:text-ink" title={t('A DAT with only the games you are missing, for other tools to look for them.')}><Icon name="download" size={14} />{t('Fixdat of what is missing')}</a>
					{/if}
				</div>
				<div class="mt-3"><Bar value={progress.have} total={progress.total} {color} height={10} label={progress.system.short} /></div>
			</div>
		{:else}
			<p class="mt-4 max-w-xl text-mute">
				{t('There is no DAT for this system yet, so its games cannot be listed.')}
				<a href="/settings/dats" class="text-ink underline underline-offset-4">{t('Download the DAT')}</a>
			</p>
		{/if}
	</header>

	{#if progress.hasDat && list}
		<div class="sticky top-0 z-20 -mx-4 flex flex-wrap items-center gap-2 bg-night/90 px-4 py-3 backdrop-blur-md sm:-mx-8 sm:px-8">
			<div class="-mx-1 flex max-w-full gap-2 overflow-x-auto px-1" role="group" aria-label={t('Show')}>
				<button class="chip" aria-pressed={filter === 'all'} onclick={() => setParam('show', null)}>{t('All')} <span class="t-num opacity-70">{n(list.total)}</span></button>
				<button class="chip" aria-pressed={filter === 'have'} onclick={() => setParam('show', 'have')}>{t('Owned')} <span class="t-num opacity-70">{n(progress.have)}</span></button>
				<button class="chip" aria-pressed={filter === 'missing'} onclick={() => setParam('show', 'missing')}>{t('Missing games')} <span class="t-num opacity-70">{n(progress.missing)}</span></button>
				{#if wantedCount}<button class="chip" aria-pressed={filter === 'wanted'} onclick={() => setParam('show', 'wanted')}><Icon name="star" size={14} filled />{t('Wishlist')} <span class="t-num opacity-70">{n(wantedCount)}</span></button>{/if}
			</div>
			<div class="ml-auto flex flex-1 flex-wrap items-center justify-end gap-2 sm:flex-none">
				<label class="relative min-w-48 flex-1 sm:flex-none">
					<span class="sr-only">{t('Search')}</span>
					<span class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint"><Icon name="search" size={16} /></span>
					<input class="field !rounded-full !py-2 pl-9" type="search" placeholder={t('Search a game')} value={searchValue} oninput={(e) => onSearch(e.currentTarget.value)} />
				</label>
				<label>
					<span class="sr-only">{t('Region')}</span>
					<select class="field !w-auto !rounded-full !py-2" value={region} onchange={(e) => setParam('region', e.currentTarget.value)}>
						<option value="">{t('All regions')}</option>
						{#each regions as r}<option value={r}>{t(r)}</option>{/each}
					</select>
				</label>
				<div class="flex rounded-full bg-raised p-1" role="group" aria-label={t('View')}>
					<button class="rounded-full p-1.5" class:bg-ink={view === 'grid'} class:text-night={view === 'grid'} aria-pressed={view === 'grid'} onclick={() => setParam('view', null)}><Icon name="grid" size={16} label={t('Covers')} /></button>
					<button class="rounded-full p-1.5" class:bg-ink={view === 'list'} class:text-night={view === 'list'} aria-pressed={view === 'list'} onclick={() => setParam('view', 'list')}><Icon name="list" size={16} label={t('List')} /></button>
				</div>
			</div>
		</div>

		{#if items.length === 0}
			<p class="py-16 text-center text-mute">
				{filter === 'missing' && !query && !region ? t('Nothing is missing here.') : t('No game matches.')}
			</p>
		{:else if view === 'list'}
			<ul class="mt-4 divide-y divide-line/60">
				{#each items.slice(0, shown) as e (e.id)}
					<li>
						<a href="/games/{e.id}" class="flex items-center gap-3 py-2.5 hover:text-gold">
							<span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background: {e.owned ? color : 'transparent'}; border: 1.5px solid {e.owned ? color : 'var(--color-faint)'}"></span>
							<span class="flex-1 truncate" class:text-mute={!e.owned}>{e.name}</span>
							{#if e.wanted && !e.owned}<span class="text-gold"><Icon name="star" size={15} filled label={t('On your wishlist')} /></span>{/if}
							{#if e.discs && !e.owned && e.discsOwned}<span class="text-sm text-warn">{t('{n} of {m} discs', { n: e.discsOwned, m: e.discs })}</span>{/if}
							{#if e.versions > 1}<span class="text-sm text-faint">{plural('{n} version', '{n} versions', e.versions)}</span>{/if}
						</a>
					</li>
				{/each}
			</ul>
		{:else}
			<ul class="mt-4 grid grid-cols-[repeat(auto-fill,minmax(132px,1fr))] gap-x-4 gap-y-6 sm:grid-cols-[repeat(auto-fill,minmax(156px,1fr))]">
				{#each items.slice(0, shown) as e (e.id)}
					<li>
						<a href="/games/{e.id}" class="group block">
							<div class="relative transition-transform duration-200 ease-[var(--ease-out-soft)] group-hover:-translate-y-1">
								<Cover system={id} libretro={progress.system.libretro} name={e.name} title={e.title} owned={e.owned} />
								{#if e.wanted && !e.owned}<span class="absolute right-1.5 top-1.5 rounded-full bg-night/85 p-1 text-gold"><Icon name="star" size={14} filled label={t('On your wishlist')} /></span>{/if}
							</div>
							<div class="mt-2 line-clamp-2 text-sm leading-snug" class:text-mute={!e.owned}>{e.title}</div>
							<div class="text-xs text-faint">{e.regions.map((r) => t(r)).join(', ')}</div>
							{#if e.discs && !e.owned && e.discsOwned}<div class="text-xs text-warn">{t('{n} of {m} discs', { n: e.discsOwned, m: e.discs })}</div>{/if}
						</a>
					</li>
				{/each}
			</ul>
		{/if}
		{#if shown < items.length}<div bind:this={sentinel} class="h-px"></div>{/if}
	{/if}

	{#if reviewCounts.bad + reviewCounts.unknown > 0}
		<section id="files" class="mt-14" aria-labelledby="files-title">
			<h2 id="files-title" class="t-section">{t('Files to review')}</h2>
			<div class="mt-4 flex gap-2" role="group">
				{#if reviewCounts.bad}<button class="chip" aria-pressed={reviewStatus === 'bad'} onclick={() => { setParam('why', null); setParam('files', 'bad'); }}>{t('Damaged or modified')} <span class="t-num opacity-70">{n(reviewCounts.bad)}</span></button>{/if}
				{#if reviewCounts.unknown}<button class="chip" aria-pressed={reviewStatus === 'unknown'} onclick={() => { setParam('why', null); setParam('files', 'unknown'); }}>{t('Not identified')} <span class="t-num opacity-70">{n(reviewCounts.unknown)}</span></button>{/if}
			</div>
			<p class="mt-3 max-w-prose text-sm text-mute">
				{reviewStatus === 'bad'
					? t('These files have the name of a game of the DAT, but their checksums are different, so they do not count as that game.')
					: t('These files are not in the DAT, so they do not count for any game. That is not always a problem: here is why each group is not recognised.')}
				{#if review?.dat}
					{t('Compared with {dat}, version {v} ({source}).', { dat: review.dat.name, v: review.dat.version || '?', source: fromLibretro ? 'libretro-database' : t('imported by you') })}
				{/if}
			</p>

			{#if shownReasons.length}
				<ul class="mt-5 grid gap-3 sm:grid-cols-2">
					{#each shownReasons as r (r)}
						<li>
							<button class="panel block h-full w-full p-4 text-left ring-1 transition-colors hover:ring-gold/60" class:ring-gold={reason === r} class:ring-transparent={reason !== r} aria-pressed={reason === r} onclick={() => setParam('why', reason === r ? null : r)}>
								<span class="flex items-baseline justify-between gap-3">
									<span class="font-semibold">{reasonText[r]?.title ?? r}</span>
									<span class="t-num text-mute">{n(review?.reasons[r] ?? 0)}</span>
								</span>
								<span class="mt-1 block text-sm text-mute">{reasonText[r]?.text}</span>
							</button>
						</li>
					{/each}
				</ul>
				{#if fromLibretro && reviewStatus === 'unknown' && ((review?.reasons.prerelease ?? 0) + (review?.reasons.homebrew ?? 0) > 0)}
					<p class="mt-3 text-sm"><a href="/settings/dats" class="text-gold underline underline-offset-4">{t('Import an official DAT')}</a></p>
				{/if}
			{/if}

			<ul class="mt-6 divide-y divide-line/60 text-sm">
				{#each review?.files ?? [] as f (f.id)}
					<li class="py-2.5">
						<div class="flex flex-wrap items-baseline gap-x-4 gap-y-1">
							<span class="min-w-0 flex-1 break-words">
								<span class="text-ink">{f.path.split(/[\\/]/).pop()}{f.entry && f.entry !== f.path.split(/[\\/]/).pop() ? ` › ${f.entry}` : ''}</span>
								<span class="block break-all text-xs text-faint">{f.path.slice(0, Math.max(0, f.path.length - (f.path.split(/[\\/]/).pop()?.length ?? 0) - 1))}</span>
							</span>
							{#if !reason && reasonText[f.reason]}<span class="text-xs text-faint">{reasonText[f.reason].title}</span>{/if}
						</div>
						{#if f.expected?.length}
							<div class="mt-1 grid gap-x-4 font-mono text-xs text-mute sm:grid-cols-[auto_1fr]">
								<span>{t('Yours')}</span><span>{bytes(f.size)} · CRC {f.crc}</span>
								{#each f.expected as e}
									<span>{t('The DAT')}</span><span class:text-warn={e.size !== f.size}>{bytes(e.size)} · CRC {e.crc.toLowerCase()} <span class="font-sans text-faint">{e.name}</span></span>
								{/each}
							</div>
						{:else}
							<div class="mt-0.5 font-mono text-xs text-faint">{bytes(f.size)} · CRC {f.crc}</div>
						{/if}
					</li>
				{/each}
			</ul>
			{#if review && review.files.length >= 1000}<p class="mt-2 text-sm text-mute">{t('The first 1,000 files are shown.')}</p>{/if}
		</section>
	{/if}
{/if}
