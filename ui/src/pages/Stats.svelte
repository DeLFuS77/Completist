<script lang="ts">
	import { get, type Stats } from '../lib/api';
	import { collection } from '../lib/state.svelte';
	import { systemColor } from '../lib/systems';
	import { t, n, bytes, relative, locale } from '../lib/i18n.svelte';
	import Bar from '../components/Bar.svelte';
	import Cover from '../components/Cover.svelte';

	let stats = $state<Stats | null>(null);
	let error = $state('');
	$effect(() => {
		collection.version;
		get<Stats>('/api/stats')
			.then((s) => (stats = s))
			.catch((e) => (error = t(e.message)));
	});

	const systemsById = $derived(Object.fromEntries((collection.overview?.systems ?? []).map((s) => [s.system.id, s.system])));
	const short = (id: string) => systemsById[id]?.short ?? id;
	const pct = (v: number) => `${n(Math.floor(v))}%`;

	// the history as a line: percent complete over the days
	const W = 640;
	const H = 180;
	const chart = $derived.by(() => {
		const h = stats?.history ?? [];
		if (h.length < 2) return null;
		const times = h.map((p) => new Date(p.day + 'T12:00:00').getTime());
		const t0 = times[0];
		const t1 = times[times.length - 1];
		const lo = Math.max(0, Math.floor(Math.min(...h.map((p) => p.percent)) / 10) * 10 - 10);
		const hi = Math.min(100, Math.ceil(Math.max(...h.map((p) => p.percent)) / 10) * 10 + 10);
		const x = (ms: number) => (t1 === t0 ? W : ((ms - t0) / (t1 - t0)) * W);
		const y = (v: number) => H - ((v - lo) / (hi - lo || 1)) * H;
		const points = h.map((p, i) => [x(times[i]), y(p.percent)] as const);
		const line = points.map(([px, py], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${py.toFixed(1)}`).join(' ');
		const area = `${line} L${W},${H} L0,${H} Z`;
		const fmt = new Intl.DateTimeFormat(locale.code, { day: 'numeric', month: 'short' });
		const first = h[0];
		const last = h[h.length - 1];
		return {
			line,
			area,
			lo,
			hi,
			from: fmt.format(new Date(t0)),
			to: fmt.format(new Date(t1)),
			gained: last.have - first.have,
			last: points[points.length - 1]
		};
	});

	const maxBytes = $derived(Math.max(1, ...(stats?.sizes ?? []).map((s) => s.bytes)));
</script>

<h1 class="t-title">{t('Statistics')}</h1>

{#if error}
	<p class="mt-6 text-bad">{error}</p>
{:else if !stats}
	<div class="mt-8 h-64 animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else if stats.summary.systems === 0}
	<p class="mt-6 max-w-prose text-mute">{t('There is nothing to count yet: download the DATs of your systems and add a library.')}</p>
{:else}
	<dl class="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
		{#each [
			{ k: t('Complete'), v: pct(stats.summary.percent), gold: stats.summary.missing === 0 },
			{ k: t('Games you have'), v: n(stats.summary.have) },
			{ k: t('Missing'), v: n(stats.summary.missing) },
			{ k: t('Files'), v: n(stats.summary.files) },
			{ k: t('Space'), v: bytes(stats.summary.bytes) },
			{ k: t('Wishlist'), v: stats.wishlist.total ? `${n(stats.wishlist.owned)} / ${n(stats.wishlist.total)}` : '–' }
		] as card}
			<div class="panel px-4 py-3">
				<dt class="text-sm text-mute">{card.k}</dt>
				<dd class="t-num mt-1 text-2xl font-semibold" class:text-gold={card.gold}>{card.v}</dd>
			</div>
		{/each}
	</dl>

	<section class="mt-12" aria-labelledby="history">
		<h2 id="history" class="t-section">{t('Over time')}</h2>
		{#if chart}
			<p class="mt-2 text-mute">
				{chart.gained > 0
					? t('{n} games more since {d}.', { n: n(chart.gained), d: chart.from })
					: chart.gained < 0
						? t('{n} games fewer since {d}.', { n: n(-chart.gained), d: chart.from })
						: t('The same games as on {d}.', { d: chart.from })}
			</p>
			<div class="panel mt-4 p-4 sm:p-6">
				<svg viewBox="-36 -8 {W + 44} {H + 30}" class="h-auto w-full" role="img" aria-label={t('How complete the collection was, day by day')}>
					<defs>
						<linearGradient id="fill" x1="0" x2="0" y1="0" y2="1">
							<stop offset="0" stop-color="var(--color-gold)" stop-opacity="0.35" />
							<stop offset="1" stop-color="var(--color-gold)" stop-opacity="0" />
						</linearGradient>
					</defs>
					{#each [chart.lo, (chart.lo + chart.hi) / 2, chart.hi] as v, i}
						{@const yy = H - (i / 2) * H}
						<line x1="0" x2={W} y1={yy} y2={yy} stroke="var(--color-line)" stroke-dasharray="3 5" />
						<text x="-8" y={yy + 4} text-anchor="end" font-size="11" fill="var(--color-faint)">{Math.round(v)}%</text>
					{/each}
					<path d={chart.area} fill="url(#fill)" />
					<path d={chart.line} fill="none" stroke="var(--color-gold)" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" />
					<circle cx={chart.last[0]} cy={chart.last[1]} r="4.5" fill="var(--color-gold)" />
					<text x="0" y={H + 22} font-size="11" fill="var(--color-faint)">{chart.from}</text>
					<text x={W} y={H + 22} font-size="11" fill="var(--color-faint)" text-anchor="end">{chart.to}</text>
				</svg>
			</div>
		{:else}
			<p class="mt-2 max-w-prose text-mute">{t('Completist notes how complete your collection is every day it changes. Come back in a few days to see the line grow.')}</p>
		{/if}
	</section>

	<div class="mt-12 grid gap-12 lg:grid-cols-2">
		{#if stats.closest.length}
			<section aria-labelledby="closest">
				<h2 id="closest" class="t-section">{t('Almost there')}</h2>
				<p class="mt-2 text-sm text-mute">{t('The systems with the fewest games left.')}</p>
				<ul class="mt-4 space-y-4">
					{#each stats.closest as c (c.system)}
						<li>
							<a href="/systems/{c.system}?show=missing" class="block hover:text-gold">
								<div class="flex items-baseline justify-between gap-3">
									<span class="font-semibold">{short(c.system)}</span>
									<span class="text-sm text-mute">{t('{n} missing', { n: n(c.missing) })} · {pct(c.percent)}</span>
								</div>
								<div class="mt-2"><Bar value={c.have} total={c.have + c.missing} color={systemColor(c.system)} label={short(c.system)} /></div>
							</a>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<section aria-labelledby="makers">
			<h2 id="makers" class="t-section">{t('By maker')}</h2>
			<ul class="mt-4 space-y-4">
				{#each stats.makers as m (m.name)}
					<li>
						<div class="flex items-baseline justify-between gap-3">
							<span class="font-semibold">{m.name}</span>
							<span class="t-num text-sm text-mute">{n(m.have)} / {n(m.total)} · {pct(m.percent)}</span>
						</div>
						<div class="mt-2"><Bar value={m.have} total={m.total} label={m.name} /></div>
					</li>
				{/each}
			</ul>
		</section>

		<section aria-labelledby="regions">
			<h2 id="regions" class="t-section">{t('By region')}</h2>
			<p class="mt-2 text-sm text-mute">{t('A game released in several regions counts in each.')}</p>
			<ul class="mt-4 space-y-4">
				{#each stats.regions.slice(0, 8) as r (r.name)}
					<li>
						<div class="flex items-baseline justify-between gap-3">
							<span class="font-semibold">{t(r.name)}</span>
							<span class="t-num text-sm text-mute">{n(r.have)} / {n(r.total)} · {pct(r.percent)}</span>
						</div>
						<div class="mt-2"><Bar value={r.have} total={r.total} label={t(r.name)} /></div>
					</li>
				{/each}
			</ul>
		</section>

		{#if stats.sizes.length}
			<section aria-labelledby="space">
				<h2 id="space" class="t-section">{t('Space by system')}</h2>
				<ul class="mt-4 space-y-3">
					{#each stats.sizes as s (s.system)}
						<li class="grid grid-cols-[8rem_1fr_auto] items-center gap-3 text-sm">
							<span class="truncate">{short(s.system)}</span>
							<span class="h-2 overflow-hidden rounded-full bg-line/70"><span class="block h-full rounded-full" style="width: {(s.bytes / maxBytes) * 100}%; background: {systemColor(s.system)}"></span></span>
							<span class="t-num text-mute">{bytes(s.bytes)}</span>
						</li>
					{/each}
				</ul>
			</section>
		{/if}
	</div>

	{#if stats.recent.length}
		<section class="mt-12" aria-labelledby="recent">
			<h2 id="recent" class="t-section">{t('Added lately')}</h2>
			<ul class="mt-4 grid grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-x-3 gap-y-5 sm:grid-cols-[repeat(auto-fill,minmax(120px,1fr))] sm:gap-x-4 sm:gap-y-6">
				{#each stats.recent as g (g.id)}
					<li>
						<a href="/games/{g.id}" class="group block">
							<div class="transition-transform duration-200 group-hover:-translate-y-1">
								<Cover system={g.system} libretro={systemsById[g.system]?.libretro ?? ''} name={g.name} title={g.title} />
							</div>
							<div class="mt-2 line-clamp-2 text-sm leading-snug">{g.title}</div>
							<div class="text-xs text-faint">{short(g.system)} · {relative(g.since)}</div>
						</a>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
{/if}
