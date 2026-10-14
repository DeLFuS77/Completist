<script lang="ts">
	import { get, post, request } from '../../lib/api';
	import { refreshCollection } from '../../lib/state.svelte';
	import { t, bytes, date } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	type Info = { name: string; size: number; time: string };
	let list = $state<Info[]>([]);
	let message = $state('');
	let error = $state('');
	let busy = $state(false);
	const load = () => get<Info[]>('/api/backups').then((l) => (list = l));
	$effect(() => {
		load();
	});

	async function make() {
		message = error = '';
		busy = true;
		try {
			await post('/api/backups');
			message = t('The copy was saved.');
			load();
		} catch (e) {
			error = t((e as Error).message);
		} finally {
			busy = false;
		}
	}
	async function restore(name: string) {
		if (!confirm(t('Restore the copy of {d}? The libraries, DATs, settings and users go back to how they were then.', { d: name.slice(18, 28) }))) return;
		await run(() => post(`/api/backups/${encodeURIComponent(name)}/restore`));
	}
	let fileInput = $state<HTMLInputElement>();
	async function upload() {
		const file = fileInput?.files?.[0];
		if (!file) return;
		if (!confirm(t('Restore this backup? The libraries, DATs, settings and users go back to how they were in it.'))) {
			fileInput!.value = '';
			return;
		}
		await run(() => request('POST', '/api/backups/restore', file));
		fileInput!.value = '';
	}
	async function run(action: () => Promise<unknown>) {
		message = error = '';
		busy = true;
		try {
			await action();
			message = t('The backup was restored. The libraries are being scanned again.');
			refreshCollection();
			load();
		} catch (e) {
			error = t((e as Error).message);
		} finally {
			busy = false;
		}
	}
</script>

<p class="max-w-prose text-mute">
	{t('A backup holds the libraries, the DATs, how games are counted, the notifications, the users and the activity log. Your ROMs are not in it. Completist saves a copy every week and keeps the last five.')}
</p>

<div class="mt-6 flex flex-wrap items-center gap-3">
	<a class="btn btn-primary" href="/api/backups/download" download><Icon name="download" />{t('Download a backup')}</a>
	<button class="btn btn-quiet" onclick={make} disabled={busy}>{t('Save a copy now')}</button>
	<label class="btn btn-quiet cursor-pointer">
		<Icon name="upload" />{t('Restore from a file')}
		<input type="file" accept=".zip" class="sr-only" bind:this={fileInput} onchange={upload} />
	</label>
</div>
{#if message}<p class="mt-3 text-sm text-good" role="status">{message}</p>{/if}
{#if error}<p class="mt-3 text-sm text-bad" role="alert">{error}</p>{/if}

<h3 class="t-section mt-10">{t('Saved copies')}</h3>
{#if list.length === 0}
	<p class="mt-3 text-mute">{t('No copy yet. The first one is saved a minute after Completist starts.')}</p>
{:else}
	<ul class="mt-4 max-w-2xl divide-y divide-line/60">
		{#each list as b (b.name)}
			<li class="flex flex-wrap items-center gap-x-4 gap-y-1 py-3">
				<span class="min-w-0 flex-1">{date(b.time)}</span>
				<span class="text-sm text-mute">{bytes(b.size)}</span>
				<a class="text-sm text-mute underline underline-offset-4 hover:text-ink" href="/api/backups/{encodeURIComponent(b.name)}" download>{t('Download')}</a>
				<button class="text-sm text-mute underline underline-offset-4 hover:text-ink" onclick={() => restore(b.name)} disabled={busy}>{t('Restore')}</button>
			</li>
		{/each}
	</ul>
{/if}
