<script lang="ts">
	import { router } from './lib/router.svelte';
	import { session, collection, live, loadSession, refreshCollection, connectTasks, running } from './lib/state.svelte';
	import { t, n, locale } from './lib/i18n.svelte';
	import { systemColor } from './lib/systems';
	import Icon from './components/Icon.svelte';
	import Home from './pages/Home.svelte';
	import System from './pages/System.svelte';
	import Game from './pages/Game.svelte';
	import Tasks from './pages/Tasks.svelte';
	import Stats from './pages/Stats.svelte';
	import Wishlist from './pages/Wishlist.svelte';
	import Organize from './pages/Organize.svelte';
	import Settings from './pages/Settings.svelte';
	import Login from './pages/Login.svelte';

	let ready = $state(false);
	let menuOpen = $state(false);

	$effect(() => {
		loadSession().then(() => {
			ready = true;
			if (!session.needLogin) {
				refreshCollection();
				connectTasks();
			}
		});
	});
	$effect(() => {
		document.documentElement.lang = locale.code;
	});
	$effect(() => {
		// the menu closes after moving to another page
		router.route;
		menuOpen = false;
	});

	const systems = $derived((collection.overview?.systems ?? []).filter((s) => s.hasDat));
	const active = $derived(running());
	const route = $derived(router.route);
	const current = (href: string) => {
		const r = route;
		if (href === '/') return r.name === 'home';
		if (href === '/tasks') return r.name === 'tasks';
		if (href === '/stats') return r.name === 'stats';
		if (href === '/wishlist') return r.name === 'wishlist';
		if (href === '/organize') return r.name === 'organize';
		if (href === '/settings') return r.name === 'settings';
		return r.name === 'system' && href === `/systems/${r.id}`;
	};
	$effect(() => {
		const r = route;
		const sys = r.name === 'system' ? systems.find((s) => s.system.id === r.id)?.system.short : '';
		const names: Record<string, string> = { tasks: t('Tasks'), settings: t('Settings'), stats: t('Statistics'), wishlist: t('Wishlist'), organize: t('Organize') };
		document.title = sys ? `${sys} – Completist` : names[r.name] ? `${names[r.name]} – Completist` : 'Completist';
	});
</script>

{#if !ready}
	<div class="min-h-dvh"></div>
{:else if session.needLogin}
	<Login />
{:else}
	<div class="flex min-h-dvh">
		<aside
			class="fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-line/60 bg-deep transition-transform duration-200 lg:sticky lg:top-0 lg:h-dvh lg:translate-x-0"
			class:-translate-x-full={!menuOpen}
			aria-label={t('Menu')}
		>
			<div class="flex items-center justify-between px-5 pb-6 pt-6">
				<a href="/" class="font-display text-xl font-bold tracking-tight">Completist</a>
				<button class="rounded-lg p-1 text-mute hover:text-ink lg:hidden" onclick={() => (menuOpen = false)} aria-label={t('Close the menu')}><Icon name="close" /></button>
			</div>
			<nav class="flex flex-1 flex-col gap-1 overflow-y-auto px-3 pb-4">
				<a href="/" class="navlink" aria-current={current('/') ? 'page' : undefined}><Icon name="home" />{t('Home')}</a>
				<a href="/stats" class="navlink" aria-current={current('/stats') ? 'page' : undefined}><Icon name="chart" />{t('Statistics')}</a>
				<a href="/wishlist" class="navlink" aria-current={current('/wishlist') ? 'page' : undefined}><Icon name="star" />{t('Wishlist')}</a>
				{#if systems.length}
					<div class="mt-5 px-3 pb-2 text-sm text-faint">{t('Systems')}</div>
					{#each systems as s (s.system.id)}
						<a href="/systems/{s.system.id}" class="navlink !py-2" aria-current={current(`/systems/${s.system.id}`) ? 'page' : undefined}>
							<span class="h-2.5 w-2.5 shrink-0 rounded-[3px]" style="background: {systemColor(s.system.id)}"></span>
							<span class="min-w-0 flex-1 truncate">{s.system.short}</span>
							<span class="t-num text-xs" class:text-gold={s.missing === 0} class:text-faint={s.missing > 0}>{n(Math.floor(s.percent))}%</span>
						</a>
					{/each}
				{/if}
				<div class="mt-auto pt-5">
					{#if session.status?.isAdmin}
						<a href="/organize" class="navlink" aria-current={current('/organize') ? 'page' : undefined}><Icon name="tidy" />{t('Organize')}</a>
					{/if}
					<a href="/tasks" class="navlink" aria-current={current('/tasks') ? 'page' : undefined}>
						<Icon name="tasks" />{t('Tasks')}
						{#if active.length}<span class="ml-auto h-2 w-2 animate-pulse rounded-full bg-gold" aria-label={t('{n} running', { n: active.length })}></span>{/if}
					</a>
					<a href="/settings" class="navlink" aria-current={current('/settings') ? 'page' : undefined}><Icon name="settings" />{t('Settings')}</a>
				</div>
			</nav>
			{#if active.length}
				<div class="border-t border-line/60 px-5 py-4 text-sm">
					<div class="truncate text-ink">{t(active[0].kind === 'scan' ? 'Scanning the libraries' : active[0].kind === 'organize' ? 'Organizing files' : 'Downloading DATs')}</div>
					<div class="mt-1 truncate text-xs text-mute">{t(active[0].message)}</div>
					{#if active[0].total > 0}
						<div class="mt-2 h-1 overflow-hidden rounded-full bg-line"><div class="h-full rounded-full bg-gold transition-[width]" style="width: {(active[0].current / active[0].total) * 100}%"></div></div>
					{/if}
				</div>
			{/if}
		</aside>
		{#if menuOpen}
			<button class="fixed inset-0 z-30 bg-black/50 lg:hidden" onclick={() => (menuOpen = false)} aria-label={t('Close the menu')}></button>
		{/if}

		<div class="min-w-0 flex-1">
			<header class="sticky top-0 z-20 flex items-center gap-3 border-b border-line/60 bg-night/90 px-4 py-3 backdrop-blur-md lg:hidden">
				<button class="rounded-lg p-1 text-mute hover:text-ink" onclick={() => (menuOpen = true)} aria-label={t('Open the menu')} aria-expanded={menuOpen}><Icon name="menu" /></button>
				<a href="/" class="font-display font-bold tracking-tight">Completist</a>
			</header>
			<main class="mx-auto max-w-[1400px] px-4 py-4 sm:px-8 sm:py-8">
				{#if collection.error && !collection.overview}
					<p class="text-bad">{t('The server does not answer: {e}', { e: collection.error })}</p>
				{/if}
				{#if route.name === 'home'}<Home />
				{:else if route.name === 'system'}{#key route.id}<System id={route.id} />{/key}
				{:else if route.name === 'game'}{#key route.id}<Game id={route.id} />{/key}
				{:else if route.name === 'tasks'}<Tasks />
				{:else if route.name === 'stats'}<Stats />
				{:else if route.name === 'wishlist'}<Wishlist />
				{:else if route.name === 'organize'}<Organize />
				{:else if route.name === 'settings'}<Settings tab={route.tab} />
				{:else}
					<h1 class="t-title">{t('This page does not exist')}</h1>
					<a href="/" class="btn btn-quiet mt-6">{t('Go to your collection')}</a>
				{/if}
			</main>
		</div>
	</div>
{/if}

<style>
	:global(.navlink) {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		border-radius: 10px;
		padding: 0.6rem 0.75rem;
		color: var(--color-mute);
		font-weight: 500;
	}
	:global(.navlink:hover) {
		color: var(--color-ink);
		background: var(--color-panel);
	}
	:global(.navlink[aria-current='page']) {
		color: var(--color-ink);
		background: var(--color-raised);
	}
</style>
