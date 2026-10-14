<script lang="ts">
	import { del, get, post, request, type SystemSetting, type DatInfo } from '../../lib/api';
	import { collection, live, refreshCollection } from '../../lib/state.svelte';
	import { t, n, date } from '../../lib/i18n.svelte';
	import { systemColor } from '../../lib/systems';
	import Icon from '../../components/Icon.svelte';

	let systems = $state<SystemSetting[]>([]);
	let message = $state('');
	let error = $state('');
	const load = () => get<SystemSetting[]>('/api/systems').then((s) => (systems = s));
	$effect(() => {
		collection.version;
		load();
	});
	const syncing = $derived(live.tasks.some((t) => t.kind === 'datsync' && t.status === 'running'));
	$effect(() => {
		// the list changes when a download ends
		if (!syncing) load();
	});

	const withFiles = $derived(new Set((collection.overview?.systems ?? []).map((s) => s.system.id)));
	let selected = $state<Set<string>>(new Set());
	let preselected = false;
	$effect(() => {
		// once, the systems with files or a DAT already
		if (preselected || !systems.length) return;
		preselected = true;
		selected = new Set(systems.filter((s) => s.dat || withFiles.has(s.id)).map((s) => s.id));
	});
	function toggle(id: string) {
		const next = new Set(selected);
		next.has(id) ? next.delete(id) : next.add(id);
		selected = next;
	}

	async function download() {
		error = message = '';
		try {
			await post('/api/dats/sync', { systems: [...selected] });
			message = t('Downloading {n} DATs. You can follow it in Tasks.', { n: n(selected.size) });
		} catch (e: any) {
			error = t(e.message);
		}
	}

	let fileInput = $state<HTMLInputElement>();
	let uploadSystem = $state('');
	async function upload() {
		const file = fileInput?.files?.[0];
		if (!file) return;
		error = message = '';
		try {
			const q = uploadSystem ? `?system=${uploadSystem}&name=${encodeURIComponent(file.name)}` : `?name=${encodeURIComponent(file.name)}`;
			const change = await request<{ system: string; games: number }>('POST', `/api/dats/import${q}`, file);
			message = t('Imported {g} games for {s}.', { g: n(change.games), s: systems.find((s) => s.id === change.system)?.short ?? change.system });
			fileInput!.value = '';
			load();
			refreshCollection();
		} catch (e: any) {
			error = t(e.message);
		}
	}
	async function remove(s: SystemSetting) {
		if (!confirm(t('Remove the DAT of {s}?', { s: s.short }))) return;
		await del(`/api/dats/${s.id}`);
		load();
		refreshCollection();
	}
</script>

<p class="max-w-prose text-mute">
	{t('A DAT lists every known good dump of a system. Completist downloads the No-Intro DATs from libretro-database and updates them every week. You can also import a DAT file of your own, from No-Intro or another group.')}
</p>

<div class="mt-6 flex flex-wrap items-center gap-3">
	<button class="btn btn-primary" onclick={download} disabled={syncing || selected.size === 0}>
		<Icon name="download" />{syncing ? t('Downloading…') : t('Download the selected DATs ({n})', { n: n(selected.size) })}
	</button>
	<label class="btn btn-quiet cursor-pointer">
		<Icon name="upload" />{t('Import a DAT file')}
		<input type="file" accept=".dat,.xml" class="sr-only" bind:this={fileInput} onchange={upload} />
	</label>
	<label class="text-sm text-mute">
		<span class="sr-only">{t('System of the file')}</span>
		<select class="field !w-auto !py-2" bind:value={uploadSystem}>
			<option value="">{t('System: from the file')}</option>
			{#each systems as s}<option value={s.id}>{s.short}</option>{/each}
		</select>
	</label>
</div>
{#if message}<p class="mt-3 text-sm text-good">{message}</p>{/if}
{#if error}<p class="mt-3 text-sm text-bad">{error}</p>{/if}

<ul class="mt-6 divide-y divide-line/60">
	{#each systems as s (s.id)}
		<li class="flex flex-wrap items-center gap-x-4 gap-y-1 py-3">
			<label class="flex min-w-0 flex-1 items-center gap-3">
				<input type="checkbox" checked={selected.has(s.id)} onchange={() => toggle(s.id)} class="accent-[var(--color-gold)]" />
				<span class="h-3 w-3 shrink-0 rounded-[4px]" style="background: {systemColor(s.id)}"></span>
				<span class="font-semibold">{s.short}</span>
				{#if withFiles.has(s.id) && !s.dat}<span class="text-sm text-warn">{t('You have files of it')}</span>{/if}
			</label>
			{#if s.dat}
				<span class="text-sm text-mute">{t('{g} games, version {v}', { g: n(s.dat.games), v: s.dat.version || '?' })}</span>
				<span class="text-sm text-faint" title={s.dat.source}>{date(s.dat.imported)}</span>
				<button class="rounded-md p-1 text-faint hover:text-bad" onclick={() => remove(s)} aria-label={t('Remove the DAT of {s}', { s: s.short })}><Icon name="trash" size={16} /></button>
			{:else}
				<span class="text-sm text-faint">{t('No DAT')}</span>
			{/if}
		</li>
	{/each}
</ul>
