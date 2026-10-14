<script lang="ts">
	import { del, get, type Wish } from '../lib/api';
	import { collection, session } from '../lib/state.svelte';
	import { router, setParam } from '../lib/router.svelte';
	import { systemColor } from '../lib/systems';
	import { t, n } from '../lib/i18n.svelte';
	import Cover from '../components/Cover.svelte';
	import Icon from '../components/Icon.svelte';

	let list = $state<Wish[] | null>(null);
	let error = $state('');
	let message = $state('');
	const load = () =>
		get<Wish[]>('/api/wishlist')
			.then((l) => (list = l))
			.catch((e) => (error = t(e.message)));
	$effect(() => {
		collection.version;
		load();
	});

	const isAdmin = $derived(session.status?.isAdmin ?? false);
	const show = $derived(router.search.get('show') ?? 'wanted');
	const systemsById = $derived(Object.fromEntries((collection.overview?.systems ?? []).map((s) => [s.system.id, s.system])));
	const wanted = $derived((list ?? []).filter((w) => !w.owned));
	const got = $derived((list ?? []).filter((w) => w.owned));
	const shown = $derived(show === 'got' ? got : wanted);
	// grouped by system, in the order of the menu
	const groups = $derived.by(() => {
		const order = (collection.overview?.systems ?? []).map((s) => s.system.id);
		const by = new Map<string, Wish[]>();
		for (const w of shown) by.set(w.system, [...(by.get(w.system) ?? []), w]);
		return [...by.entries()].sort((a, b) => order.indexOf(a[0]) - order.indexOf(b[0]));
	});

	async function remove(w: Wish) {
		await del(`/api/wishlist/${encodeURIComponent(w.system)}/${encodeURIComponent(w.name)}`);
		load();
	}
	async function removeGot() {
		if (!confirm(t('Remove from the wishlist the {n} games you have now?', { n: n(got.length) }))) return;
		for (const w of got) await del(`/api/wishlist/${encodeURIComponent(w.system)}/${encodeURIComponent(w.name)}`);
		load();
	}
	async function copy() {
		const text = groups.map(([sys, items]) => `${systemsById[sys]?.short ?? sys}\n${items.map((w) => `- ${w.name}`).join('\n')}`).join('\n\n');
		try {
			await navigator.clipboard.writeText(text);
			message = t('The list was copied.');
		} catch {
			message = t('The list could not be copied.');
		}
		setTimeout(() => (message = ''), 3000);
	}
</script>

<h1 class="t-title">{t('Wishlist')}</h1>
<p class="mt-2 max-w-prose text-mute">
	{t('The games you are looking for. Add one from its page; when it reaches your collection, it moves to the games you got, and a notification can tell you.')}
</p>

{#if error}
	<p class="mt-6 text-bad">{error}</p>
{:else if !list}
	<div class="mt-8 h-48 animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else if list.length === 0}
	<div class="panel mt-8 max-w-xl p-6">
		<p>{t('Your wishlist is empty.')}</p>
		<p class="mt-2 text-sm text-mute">{t('Open a system, choose a missing game and press "Add to the wishlist".')}</p>
	</div>
{:else}
	<div class="mt-6 flex flex-wrap items-center gap-2">
		<button class="chip" aria-pressed={show === 'wanted'} onclick={() => setParam('show', null)}>{t('Looking for')} <span class="t-num opacity-70">{n(wanted.length)}</span></button>
		<button class="chip" aria-pressed={show === 'got'} onclick={() => setParam('show', 'got')}>{t('Got them')} <span class="t-num opacity-70">{n(got.length)}</span></button>
		<div class="ml-auto flex flex-wrap gap-2">
			{#if shown.length}<button class="btn btn-quiet" onclick={copy}>{t('Copy the list')}</button>{/if}
			{#if isAdmin && show === 'got' && got.length}<button class="btn btn-quiet" onclick={removeGot}>{t('Remove the ones you have')}</button>{/if}
		</div>
	</div>
	{#if message}<p class="mt-3 text-sm text-good" role="status">{message}</p>{/if}

	{#if groups.length === 0}
		<p class="py-16 text-center text-mute">{show === 'got' ? t('None of them yet. Keep looking!') : t('You have every game of your wishlist.')}</p>
	{/if}
	{#each groups as [sys, items] (sys)}
		<section class="mt-10" aria-labelledby="w-{sys}">
			<h2 id="w-{sys}" class="flex items-center gap-2 t-section">
				<span class="h-2.5 w-2.5 rounded-[3px]" style="background: {systemColor(sys)}"></span>{systemsById[sys]?.short ?? sys}
				<span class="t-num text-sm font-normal text-faint">{n(items.length)}</span>
			</h2>
			<ul class="mt-4 grid grid-cols-[repeat(auto-fill,minmax(132px,1fr))] gap-x-4 gap-y-6 sm:grid-cols-[repeat(auto-fill,minmax(156px,1fr))]">
				{#each items as w (w.name)}
					<li class="group relative">
						<a href={w.inDat ? `/games/${w.id}` : undefined} class="block">
							<Cover system={sys} libretro={systemsById[sys]?.libretro ?? ''} name={w.name} title={w.title} />
							<div class="mt-2 line-clamp-2 text-sm leading-snug">{w.title}</div>
							<div class="text-xs text-faint">{w.inDat ? w.regions.map((r) => t(r)).join(', ') : t('No longer in the DAT')}</div>
						</a>
						{#if w.owned}<span class="absolute left-1.5 top-1.5 rounded-full bg-good px-2 py-0.5 text-xs font-semibold text-night">{t('Got it')}</span>{/if}
						{#if isAdmin}
							<button class="absolute right-1.5 top-1.5 rounded-full bg-night/85 p-1.5 text-mute opacity-100 hover:text-ink sm:opacity-0 sm:group-hover:opacity-100 sm:focus-visible:opacity-100" onclick={() => remove(w)} aria-label={t('Remove {name} from the wishlist', { name: w.title })}>
								<Icon name="close" size={14} />
							</button>
						{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/each}
{/if}
