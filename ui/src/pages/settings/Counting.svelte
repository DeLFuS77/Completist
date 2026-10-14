<script lang="ts">
	import { get, put, type Mode, type Preferences, type SystemSetting } from '../../lib/api';
	import { refreshCollection } from '../../lib/state.svelte';
	import { t } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	let prefs = $state<Preferences | null>(null);
	let systems = $state<SystemSetting[]>([]);
	let saved = $state(false);
	let error = $state('');
	$effect(() => {
		get<Preferences>('/api/preferences').then((p) => (prefs = p));
		get<SystemSetting[]>('/api/systems').then((s) => (systems = s.filter((x) => x.dat)));
	});

	const modes: { id: Mode; name: string; text: string }[] = [
		{ id: '1g1r', name: 'One version of each game', text: 'A game counts once, whatever its region or revision: having it from any region is enough. Betas, demos, hacks and unlicensed games are left out.' },
		{ id: 'official', name: 'Every official release', text: 'Each region and revision of the official games counts on its own. Betas, demos, hacks and unlicensed games are left out.' },
		{ id: 'full', name: 'Every entry of the DAT', text: 'The whole set, prototypes, demos, unlicensed and homebrew games included.' }
	];
	const allRegions = ['Europe', 'World', 'USA', 'Japan', 'Spain', 'France', 'Germany', 'Italy', 'UK', 'Australia', 'Brazil', 'Korea', 'China', 'Asia'];

	async function save() {
		if (!prefs) return;
		error = '';
		try {
			prefs = await put<Preferences>('/api/preferences', prefs);
			saved = true;
			setTimeout(() => (saved = false), 2000);
			refreshCollection();
		} catch (e: any) {
			error = t(e.message);
		}
	}
	function move(i: number, by: number) {
		if (!prefs) return;
		const r = [...prefs.regions];
		const j = i + by;
		if (j < 0 || j >= r.length) return;
		[r[i], r[j]] = [r[j], r[i]];
		prefs.regions = r;
		save();
	}
	function addRegion(region: string) {
		if (!prefs || !region || prefs.regions.includes(region)) return;
		prefs.regions = [...prefs.regions, region];
		save();
	}
	function removeRegion(i: number) {
		if (!prefs) return;
		prefs.regions = prefs.regions.filter((_, k) => k !== i);
		save();
	}
	function setSystemMode(id: string, mode: string) {
		if (!prefs) return;
		const m = { ...prefs.modes };
		if (mode) m[id] = mode as Mode;
		else delete m[id];
		prefs.modes = m;
		save();
	}
</script>

{#if prefs}
	<section>
		<h3 class="t-section">{t('What a complete set is')}</h3>
		<div class="mt-4 grid gap-3 md:grid-cols-3">
			{#each modes as m}
				<label class="panel flex cursor-pointer flex-col gap-2 border-2 p-5 transition-colors" class:border-gold={prefs.defaultMode === m.id} class:border-transparent={prefs.defaultMode !== m.id}>
					<span class="flex items-center gap-2 font-semibold">
						<input type="radio" name="mode" value={m.id} checked={prefs.defaultMode === m.id} onchange={() => { prefs!.defaultMode = m.id; save(); }} class="accent-[var(--color-gold)]" />
						{t(m.name)}
					</span>
					<span class="text-sm text-mute">{t(m.text)}</span>
				</label>
			{/each}
		</div>
		{#if saved}<p class="mt-3 text-sm text-good" role="status">{t('Saved')}</p>{/if}
		{#if error}<p class="mt-3 text-sm text-bad">{error}</p>{/if}
	</section>

	<section class="mt-10">
		<h3 class="t-section">{t('Preferred regions')}</h3>
		<p class="mt-1 max-w-prose text-sm text-mute">{t('With one version of each game, the cover shown for a missing game is from the first of these regions it was released in.')}</p>
		<ol class="mt-4 max-w-md space-y-2">
			{#each prefs.regions as r, i (r)}
				<li class="panel flex items-center gap-3 px-4 py-2">
					<span class="t-num w-5 text-mute">{i + 1}</span>
					<span class="flex-1">{t(r)}</span>
					<button class="rounded p-1 text-mute hover:text-ink disabled:opacity-30" disabled={i === 0} onclick={() => move(i, -1)} aria-label={t('Move up')}><Icon name="up" size={16} /></button>
					<button class="rounded p-1 text-mute hover:text-ink disabled:opacity-30" disabled={i === prefs.regions.length - 1} onclick={() => move(i, 1)} aria-label={t('Move down')}><Icon name="down" size={16} /></button>
					<button class="rounded p-1 text-mute hover:text-bad" onclick={() => removeRegion(i)} aria-label={t('Remove {name}', { name: t(r) })}><Icon name="close" size={16} /></button>
				</li>
			{/each}
		</ol>
		<label class="mt-3 block max-w-md">
			<span class="sr-only">{t('Add a region')}</span>
			<select class="field" onchange={(e) => { addRegion(e.currentTarget.value); e.currentTarget.value = ''; }}>
				<option value="">{t('Add a region')}</option>
				{#each allRegions.filter((r) => !prefs!.regions.includes(r)) as r}<option value={r}>{t(r)}</option>{/each}
			</select>
		</label>
	</section>

	{#if systems.length}
		<section class="mt-10">
			<h3 class="t-section">{t('By system')}</h3>
			<p class="mt-1 text-sm text-mute">{t('A system can be counted in its own way, for example every entry of the DAT for the one you collect completely.')}</p>
			<ul class="mt-4 max-w-xl divide-y divide-line/60">
				{#each systems as s (s.id)}
					<li class="flex items-center justify-between gap-4 py-2">
						<span>{s.short}</span>
						<select class="field !w-auto !py-1.5 text-sm" value={prefs.modes[s.id] ?? ''} onchange={(e) => setSystemMode(s.id, e.currentTarget.value)}>
							<option value="">{t('Like the others')}</option>
							{#each modes as m}<option value={m.id}>{t(m.name)}</option>{/each}
						</select>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
{/if}
