<script lang="ts">
	import { get } from '../lib/api';
	import { collection, session } from '../lib/state.svelte';
	import { systemColor } from '../lib/systems';
	import { t, n, bytes } from '../lib/i18n.svelte';

	type Amount = { files: number; bytes: number };
	type SystemSpace = {
		system: string;
		total: Amount;
		formats: ({ format: string } & Amount)[];
		duplicates: Amount;
		unknown: Amount;
		bad: Amount;
		compressible: Amount;
		compressTo?: string;
	};
	let space = $state<{ total: Amount; systems: SystemSpace[] } | null>(null);
	let error = $state('');
	$effect(() => {
		collection.version;
		get<{ total: Amount; systems: SystemSpace[] }>('/api/space')
			.then((s) => (space = s))
			.catch((e) => (error = t(e.message)));
	});

	const isAdmin = $derived(session.status?.isAdmin ?? false);
	const short = (id: string) => (id ? (collection.overview?.systems.find((s) => s.system.id === id)?.system.short ?? id) : t('No system'));
	const maxBytes = $derived(Math.max(1, ...(space?.systems ?? []).map((s) => s.total.bytes)));
	const sum = (key: 'duplicates' | 'unknown' | 'compressible') => (space?.systems ?? []).reduce((a, s) => a + s[key].bytes, 0);
	const into: Record<string, string> = { zip: 'zip', chd: 'CHD', cso: 'CSO', gcz: 'GCZ' };
	const kindParam: Record<string, string> = { zip: 'zip', chd: 'chd', cso: 'cso', gcz: 'gcz' };
</script>

<h1 class="t-title">{t('Space')}</h1>
<p class="mt-2 max-w-prose text-mute">{t('What your libraries take on the disk, system by system, and where space can be won back.')}</p>

{#if error}
	<p class="mt-6 text-bad">{error}</p>
{:else if !space}
	<div class="mt-8 h-48 animate-pulse rounded-3xl bg-panel/50" aria-busy="true"></div>
{:else if space.total.files === 0}
	<p class="mt-6 text-mute">{t('No file in the libraries yet.')}</p>
{:else}
	<dl class="mt-8 grid grid-cols-2 gap-3 sm:grid-cols-4">
		<div class="panel px-4 py-3"><dt class="text-sm text-mute">{t('Total')}</dt><dd class="t-num mt-1 text-2xl font-semibold">{bytes(space.total.bytes)}</dd></div>
		<div class="panel px-4 py-3"><dt class="text-sm text-mute">{t('Can be compressed')}</dt><dd class="t-num mt-1 text-2xl font-semibold">{bytes(sum('compressible'))}</dd></div>
		<div class="panel px-4 py-3"><dt class="text-sm text-mute">{t('Duplicates')}</dt><dd class="t-num mt-1 text-2xl font-semibold">{bytes(sum('duplicates'))}</dd></div>
		<div class="panel px-4 py-3"><dt class="text-sm text-mute">{t('Not identified')}</dt><dd class="t-num mt-1 text-2xl font-semibold">{bytes(sum('unknown'))}</dd></div>
	</dl>

	<ul class="mt-10 space-y-6">
		{#each space.systems as s (s.system)}
			<li>
				<div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
					<span class="flex items-center gap-2 font-semibold"><span class="h-2.5 w-2.5 rounded-[3px]" style="background: {systemColor(s.system)}"></span>{short(s.system)}</span>
					<span class="t-num text-sm text-mute">{bytes(s.total.bytes)} · {n(s.total.files)} {t('files')}</span>
				</div>
				<div class="mt-2 h-2.5 overflow-hidden rounded-full bg-line/70">
					<div class="h-full rounded-full" style="width: {(s.total.bytes / maxBytes) * 100}%; background: {systemColor(s.system)}"></div>
				</div>
				<div class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm text-mute">
					<span>{s.formats.map((f) => `${f.format.toUpperCase()} ${bytes(f.bytes)}`).join(' · ')}</span>
					{#if s.duplicates.files}<span class="text-warn">{t('{b} in duplicates', { b: bytes(s.duplicates.bytes) })}</span>{/if}
					{#if s.unknown.files}<span>{t('{b} not identified', { b: bytes(s.unknown.bytes) })}</span>{/if}
					{#if s.bad.files}<span class="text-bad">{t('{b} damaged or modified', { b: bytes(s.bad.bytes) })}</span>{/if}
					{#if s.compressible.files && s.compressTo}
						{#if isAdmin}
							<a class="text-gold underline underline-offset-4" href="/organize?system={s.system}&kind={kindParam[s.compressTo]}">{t('{b} can go into {f}', { b: bytes(s.compressible.bytes), f: into[s.compressTo] })}</a>
						{:else}
							<span class="text-gold">{t('{b} can go into {f}', { b: bytes(s.compressible.bytes), f: into[s.compressTo] })}</span>
						{/if}
					{/if}
				</div>
			</li>
		{/each}
	</ul>
{/if}
