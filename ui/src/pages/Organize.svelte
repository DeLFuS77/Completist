<script lang="ts">
	import { get, post } from '../lib/api';
	import { collection, running } from '../lib/state.svelte';
	import { router, setParam } from '../lib/router.svelte';
	import { systemColor } from '../lib/systems';
	import { t, n, bytes, date } from '../lib/i18n.svelte';
	import Icon from '../components/Icon.svelte';

	type Move = { from: string; to: string };
	type Action = { id: string; game: string; gameId: number; moves: Move[]; sheet?: Move; entry?: string; sources?: string[] };
	type Plan = { kind: string; system: string; actions: Action[]; skipped: { path: string; game: string; reason: string }[]; done: number };
	type Batch = { id: number; time: string; kind: string; system: string; user?: string; games: number; undone: boolean; saved?: number; changes: unknown[] };

	const systems = $derived((collection.overview?.systems ?? []).filter((s) => s.files.ok > 0));
	const system = $derived(router.search.get('system') ?? systems[0]?.system.id ?? '');
	const current = $derived(systems.find((s) => s.system.id === system)?.system);
	const isDisc = $derived(current?.plugin === 'disc');
	const kind = $derived.by(() => {
		const k = router.search.get('kind');
		if (k === 'zip' && !isDisc) return 'zip';
		if (k === 'chd' && isDisc && current?.id !== 'gamecube' && current?.id !== 'wii') return 'chd';
		return 'rename';
	});

	let plan = $state<Plan | null>(null);
	let history = $state<Batch[]>([]);
	let error = $state('');
	let message = $state('');
	let chosen = $state<Record<string, boolean>>({});
	let removeOriginals = $state(true);
	const busy = $derived(running().some((r) => r.kind === 'organize'));

	$effect(() => {
		collection.version;
		const sys = system;
		const k = kind;
		error = '';
		if (!sys) return;
		get<Plan>(`/api/organize?system=${encodeURIComponent(sys)}&kind=${k}`)
			.then((p) => {
				if (sys !== system || k !== kind) return;
				plan = p;
				chosen = Object.fromEntries(p.actions.map((a) => [a.id, true]));
			})
			.catch((e) => (error = t(e.message)));
		get<Batch[]>('/api/organize/history').then((h) => (history = h));
	});

	const count = $derived(plan ? plan.actions.filter((a) => chosen[a.id]).length : 0);
	const base = (p: string) => p.split(/[\\/]/).pop() ?? p;
	const short = (id: string) => collection.overview?.systems.find((s) => s.system.id === id)?.system.short ?? id;

	async function apply() {
		if (!plan) return;
		const ids = plan.actions.filter((a) => chosen[a.id]).map((a) => a.id);
		const question =
			kind === 'chd'
				? removeOriginals
					? t('Make {n} CHD images and remove the originals once each one is checked? It takes a while for big discs.', { n: n(ids.length) })
					: t('Make {n} CHD images, keeping the originals? It takes a while for big discs.', { n: n(ids.length) })
				: kind === 'zip'
				? removeOriginals
					? t('Pack {n} games in zip files and remove the originals once each zip is checked?', { n: n(ids.length) })
					: t('Pack {n} games in zip files, keeping the originals?', { n: n(ids.length) })
				: t('Rename the files of {n} games?', { n: n(ids.length) });
		if (!confirm(question)) return;
		message = error = '';
		try {
			await post('/api/organize', { system, kind, ids, removeOriginals });
			message = t('Working on it: the task list shows how it goes. It can be undone below.');
		} catch (e) {
			error = t((e as Error).message);
		}
	}
	async function undo(b: Batch) {
		if (!confirm(t('Undo this change? The files go back to how they were.'))) return;
		message = error = '';
		try {
			await post(`/api/organize/history/${b.id}/undo`);
			message = t('Undoing it.');
		} catch (e) {
			error = t((e as Error).message);
		}
	}
	function toggleAll(on: boolean) {
		chosen = Object.fromEntries((plan?.actions ?? []).map((a) => [a.id, on]));
	}
</script>

<h1 class="t-title">{t('Organize')}</h1>
<p class="mt-2 max-w-prose text-mute">
	{t('Give your files the names of the DATs, pack cartridge dumps in zip files, or turn disc images into CHD. Only verified files are changed, every change can be undone, and a new file is checked against the DAT before its original is removed.')}
</p>

{#if systems.length === 0}
	<p class="mt-8 text-mute">{t('No verified file yet: once your games are found, they can be organized here.')}</p>
{:else}
	<div class="mt-6 flex flex-wrap items-center gap-3">
		<label>
			<span class="sr-only">{t('System')}</span>
			<select class="field !w-auto" value={system} onchange={(e) => setParam('system', e.currentTarget.value)}>
				{#each systems as s (s.system.id)}<option value={s.system.id}>{s.system.short}</option>{/each}
			</select>
		</label>
		<div class="flex gap-2" role="group" aria-label={t('Change')}>
			<button class="chip" aria-pressed={kind === 'rename'} onclick={() => setParam('kind', null)}>{t('Rename')}</button>
			{#if isDisc}
				<button class="chip" aria-pressed={kind === 'chd'} onclick={() => setParam('kind', 'chd')} disabled={current?.id === 'gamecube' || current?.id === 'wii'} title={current?.id === 'gamecube' || current?.id === 'wii' ? t('Dolphin does not read CHD: RVZ is its compressed format.') : undefined}>{t('Make CHD')}</button>
			{:else}
				<button class="chip" aria-pressed={kind === 'zip'} onclick={() => setParam('kind', 'zip')}>{t('Pack in zip')}</button>
			{/if}
		</div>
	</div>

	{#if error}<p class="mt-4 text-bad" role="alert">{error}</p>{/if}
	{#if message}<p class="mt-4 text-good" role="status">{message}</p>{/if}

	{#if plan}
		<section class="mt-8" aria-labelledby="plan">
			<h2 id="plan" class="t-section flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-[3px]" style="background: {systemColor(system)}"></span>{short(system)}</h2>
			<p class="mt-2 text-sm text-mute">
				{kind === 'zip'
					? t('{a} games to pack, {d} packed already, {s} left as they are.', { a: n(plan.actions.length), d: n(plan.done), s: n(plan.skipped.length) })
					: kind === 'chd'
						? t('{a} games to turn into CHD, {d} in CHD already, {s} left as they are.', { a: n(plan.actions.length), d: n(plan.done), s: n(plan.skipped.length) })
						: t('{a} games to rename, {d} named as the DAT says already, {s} left as they are.', { a: n(plan.actions.length), d: n(plan.done), s: n(plan.skipped.length) })}
			</p>

			{#if plan.actions.length}
				<div class="mt-4 flex flex-wrap items-center gap-4">
					<button class="btn btn-primary" onclick={apply} disabled={busy || count === 0}>
						<Icon name="tidy" />{kind === 'zip' ? t('Pack {n} games', { n: n(count) }) : kind === 'chd' ? t('Make {n} CHD', { n: n(count) }) : t('Rename {n} games', { n: n(count) })}
					</button>
					{#if kind === 'zip' || kind === 'chd'}
						<label class="flex items-center gap-2 text-sm">
							<input type="checkbox" bind:checked={removeOriginals} class="accent-[var(--color-gold)]" />
							{kind === 'zip' ? t('Remove each original once its zip is checked') : t('Remove the originals once each CHD is checked')}
						</label>
					{/if}
					<span class="text-sm text-mute">
						<button class="underline underline-offset-4 hover:text-ink" onclick={() => toggleAll(true)}>{t('All')}</button> ·
						<button class="underline underline-offset-4 hover:text-ink" onclick={() => toggleAll(false)}>{t('None')}</button>
					</span>
				</div>
				<ul class="mt-4 max-h-[32rem] divide-y divide-line/60 overflow-y-auto rounded-xl border border-line/60 text-sm">
					{#each plan.actions as a (a.id)}
						<li>
							<label class="flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-panel/60">
								<input type="checkbox" bind:checked={chosen[a.id]} class="mt-1 accent-[var(--color-gold)]" />
								<span class="min-w-0 flex-1">
									<span class="block font-semibold">{a.game}</span>
									{#each a.moves as m}
										<span class="block break-all text-mute">{base(m.from)} <span class="text-faint">→</span> <span class="text-ink">{base(m.to)}</span></span>
									{/each}
									{#if a.sources?.length}<span class="block break-all text-faint">{a.sources.join(', ')}</span>{/if}
									{#if a.sheet}<span class="block break-all text-mute">{base(a.sheet.from)} <span class="text-faint">→</span> <span class="text-ink">{base(a.sheet.to)}</span> <span class="text-faint">({t('updated with the new names')})</span></span>{/if}
								</span>
							</label>
						</li>
					{/each}
				</ul>
			{/if}

			{#if plan.skipped.length}
				<details class="mt-6">
					<summary class="cursor-pointer text-sm text-mute hover:text-ink">{t('{n} files left as they are', { n: n(plan.skipped.length) })}</summary>
					<ul class="mt-3 space-y-2 text-sm">
						{#each plan.skipped as s}
							<li><span class="break-all">{base(s.path)}</span> <span class="text-mute">— {t(s.reason)}</span></li>
						{/each}
					</ul>
				</details>
			{/if}
		</section>
	{/if}

	{#if history.length}
		<section class="mt-12" aria-labelledby="history">
			<h2 id="history" class="t-section">{t('Changes made')}</h2>
			<ul class="mt-4 max-w-3xl divide-y divide-line/60">
				{#each history as b (b.id)}
					<li class="flex flex-wrap items-center gap-x-4 gap-y-1 py-3 text-sm">
						<span class="min-w-0 flex-1">
							<span class="text-ink">{b.kind === 'zip' ? t('Packed {n} games of {s}', { n: n(b.games), s: short(b.system) }) : b.kind === 'chd' ? t('Made {n} CHD of {s}', { n: n(b.games), s: short(b.system) }) : t('Renamed {n} games of {s}', { n: n(b.games), s: short(b.system) })}</span>
							{#if b.saved}<span class="text-good"> · {t('{b} saved', { b: bytes(b.saved) })}</span>{/if}
							<span class="block text-mute">{date(b.time)}{b.user ? ` · ${b.user}` : ''}</span>
						</span>
						{#if b.undone}
							<span class="text-mute">{t('Undone')}</span>
						{:else}
							<button class="btn btn-quiet !py-1.5" onclick={() => undo(b)} disabled={busy}><Icon name="undo" size={16} />{t('Undo')}</button>
						{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/if}
{/if}
