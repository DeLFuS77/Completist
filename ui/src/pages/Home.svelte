<script lang="ts">
	import { get, post, type MosaicRow } from '../lib/api';
	import { collection, running } from '../lib/state.svelte';
	import { systemColor } from '../lib/systems';
	import { t, n, plural, bytes } from '../lib/i18n.svelte';
	import Mosaic from '../components/Mosaic.svelte';
	import SystemCard from '../components/SystemCard.svelte';
	import Icon from '../components/Icon.svelte';
	import { session } from '../lib/state.svelte';

	let mosaic = $state<MosaicRow[]>([]);
	const overview = $derived(collection.overview);
	const summary = $derived(overview?.summary);
	const withDat = $derived(overview?.systems.filter((s) => s.hasDat) ?? []);
	const withoutDat = $derived(overview?.systems.filter((s) => !s.hasDat) ?? []);
	const names = $derived(Object.fromEntries((overview?.systems ?? []).map((s) => [s.system.id, s.system.short])));
	const modeText = $derived.by(() => {
		const modes = new Set(withDat.map((s) => s.mode));
		if (modes.size !== 1) return t('counted the way you chose for each system');
		const m = [...modes][0];
		return m === '1g1r' ? t('counting one version of each game') : m === 'official' ? t('counting every official release') : t('counting every entry of the DATs');
	});
	const scanning = $derived(running().some((r) => r.kind === 'scan'));
	const syncing = $derived(running().some((r) => r.kind === 'datsync'));
	const isAdmin = $derived(session.status?.isAdmin ?? false);

	$effect(() => {
		collection.version;
		get<MosaicRow[]>('/api/mosaic').then((m) => (mosaic = m)).catch(() => {});
	});

	// what is left to set up, in order
	const needsLibrary = $derived(overview && overview.libraries === 0);
	const needsDats = $derived(overview && overview.dats === 0);

	let busy = $state(false);
	let message = $state('');
	async function downloadDats() {
		busy = true;
		message = '';
		try {
			await post('/api/dats/sync', {});
		} catch (e: any) {
			message = t(e.message);
		} finally {
			busy = false;
		}
	}
	async function scan() {
		try {
			await post('/api/scan');
		} catch (e: any) {
			message = t(e.message);
		}
	}

	const issues = $derived.by(() => {
		if (!overview) return [];
		const by = (key: 'bad' | 'duplicates' | 'unknown') =>
			overview.systems
				.map((s) => ({ id: s.system.id, count: key === 'unknown' ? s.files.unknown + s.files.noDat : s.files[key] }))
				.filter((x) => x.count > 0)
				.sort((a, b) => b.count - a.count);
		return [
			{ key: 'bad', label: plural('{n} damaged or modified file', '{n} damaged or modified files', summary?.bad ?? 0), count: summary?.bad ?? 0, first: by('bad')[0]?.id },
			{ key: 'duplicates', label: plural('{n} duplicate', '{n} duplicates', summary?.duplicates ?? 0), count: summary?.duplicates ?? 0, first: by('duplicates')[0]?.id },
			{ key: 'unknown', label: plural('{n} file not identified', '{n} files not identified', summary?.unknown ?? 0), count: summary?.unknown ?? 0, first: by('unknown')[0]?.id }
		].filter((i) => i.count > 0);
	});
</script>

{#if !overview}
	<div class="h-[60vh] animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else if needsLibrary || needsDats}
	<section class="mx-auto max-w-2xl py-10">
		<h1 class="t-title">{t('Let’s find out what your collection is missing')}</h1>
		<p class="mt-3 max-w-prose text-lg text-mute">
			{t('Completist compares your ROMs with the DATs, the lists of every known good dump, and shows what you have, what is missing and what is damaged.')}
		</p>
		<ol class="mt-10 space-y-4">
			<li class="panel flex items-start gap-4 p-5" class:opacity-60={!needsLibrary}>
				<span class="t-num grid h-9 w-9 shrink-0 place-items-center rounded-full bg-raised text-sm font-semibold" class:bg-gold={!needsLibrary} class:text-night={!needsLibrary}>
					{#if needsLibrary}1{:else}<Icon name="check" size={16} />{/if}
				</span>
				<div class="flex-1">
					<h2 class="t-section">{t('Add the folder with your ROMs')}</h2>
					<p class="mt-1 text-mute">{t('Folders named after a system, such as snes or Mega Drive, help to tell the files apart.')}</p>
					{#if needsLibrary && isAdmin}
						<a href="/settings/libraries" class="btn btn-primary mt-4"><Icon name="folder" />{t('Add a library')}</a>
					{/if}
				</div>
			</li>
			<li class="panel flex items-start gap-4 p-5" class:opacity-60={!needsDats}>
				<span class="t-num grid h-9 w-9 shrink-0 place-items-center rounded-full bg-raised text-sm font-semibold" class:bg-gold={!needsDats} class:text-night={!needsDats}>
					{#if needsDats}2{:else}<Icon name="check" size={16} />{/if}
				</span>
				<div class="flex-1">
					<h2 class="t-section">{t('Download the DATs of your systems')}</h2>
					<p class="mt-1 text-mute">{t('The DATs come from No-Intro and Redump through libretro-database. Completist picks the systems found in your folders.')}</p>
					{#if needsDats && !needsLibrary && isAdmin}
						<div class="mt-4 flex flex-wrap gap-3">
							<button class="btn btn-primary" onclick={downloadDats} disabled={busy || syncing || scanning}>
								<Icon name="download" />{syncing ? t('Downloading…') : t('Download the DATs')}
							</button>
							<a class="btn btn-quiet" href="/settings/dats">{t('Choose the systems')}</a>
						</div>
						{#if scanning}<p class="mt-3 text-sm text-mute">{t('Wait for the scan to finish, so the systems of your files are known.')}</p>{/if}
					{/if}
				</div>
			</li>
			<li class="panel flex items-start gap-4 p-5" class:opacity-60={needsLibrary || needsDats}>
				<span class="t-num grid h-9 w-9 shrink-0 place-items-center rounded-full bg-raised text-sm font-semibold">3</span>
				<div>
					<h2 class="t-section">{t('See what is missing')}</h2>
					<p class="mt-1 text-mute">{t('Your collection appears here, system by system, and stays up to date as files change.')}</p>
				</div>
			</li>
		</ol>
		{#if message}<p class="mt-4 text-bad">{message}</p>{/if}
	</section>
{:else}
	<section class="pt-2">
		<h1 class="t-hero">
			{t('{p}% complete', { p: n(Math.floor(summary!.percent)) })}
		</h1>
		<p class="mt-4 max-w-2xl text-lg text-mute">
			{#if summary!.missing === 0}
				{t('You have every game of your {s} systems, {mode}.', { s: n(summary!.systems), mode: modeText })}
			{:else}
				{summary!.systems === 1
					? t('{m} games missing in 1 system, {mode}.', { m: n(summary!.missing), mode: modeText })
					: t('{m} games missing in {s} systems, {mode}.', { m: n(summary!.missing), s: n(summary!.systems), mode: modeText })}
			{/if}
			<a href="/settings/counting" class="text-ink underline decoration-line underline-offset-4 hover:decoration-gold">{t('Change how they are counted')}</a>
		</p>

		{#if mosaic.length}
			<div class="panel mt-8 p-5 sm:p-6">
				<Mosaic rows={mosaic} {names} />
				<ul class="mt-5 flex flex-wrap gap-x-5 gap-y-2 text-sm">
					{#each withDat as s (s.system.id)}
						<li>
							<a href="/systems/{s.system.id}" class="inline-flex items-center gap-2 text-mute hover:text-ink">
								<span class="h-2.5 w-2.5 rounded-[3px]" style="background: {systemColor(s.system.id)}"></span>
								{s.system.short}
								<span class="t-num text-ink">{n(s.have)}</span>
							</a>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</section>

	{#if issues.length}
		<section class="mt-10" aria-labelledby="review">
			<h2 id="review" class="t-section">{t('To review')}</h2>
			<ul class="mt-4 flex flex-wrap gap-3">
				{#each issues as issue (issue.key)}
					<li>
						<a href={issue.key === 'duplicates' ? `/systems/${issue.first}` : `/systems/${issue.first}?files=${issue.key}#files`} class="chip border-warn/40 text-ink hover:border-warn">
							<span class="h-2 w-2 rounded-full bg-warn"></span>{issue.label}
						</a>
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	<section class="mt-10" aria-labelledby="systems">
		<div class="flex flex-wrap items-baseline justify-between gap-3">
			<h2 id="systems" class="t-section">{t('Progress by system')}</h2>
			<span class="text-sm text-mute">{plural('{n} file', '{n} files', summary!.files)}, {bytes(summary!.bytes)}</span>
		</div>
		<div class="mt-4 grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-4">
			{#each withDat as p (p.system.id)}
				<SystemCard {p} />
			{/each}
		</div>
		{#if withoutDat.length}
			<p class="mt-6 text-sm text-mute">
				{t('Files of {list} were found, but there is no DAT for them yet.', { list: withoutDat.map((s) => s.system.short).join(', ') })}
				{#if isAdmin}<a href="/settings/dats" class="text-ink underline underline-offset-4">{t('Download their DATs')}</a>{/if}
			</p>
		{/if}
	</section>

	{#if isAdmin}
		<div class="mt-10">
			<button class="btn btn-quiet" onclick={scan} disabled={scanning}>
				<Icon name="refresh" />{scanning ? t('Scanning…') : t('Scan the libraries again')}
			</button>
		</div>
	{/if}
{/if}
