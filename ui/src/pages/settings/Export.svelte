<script lang="ts">
	import { get, type Library } from '../../lib/api';
	import { t } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	let libraries = $state<Library[]>([]);
	$effect(() => {
		get<Library[]>('/api/libraries').then((l) => {
			libraries = l;
			if (!from && l.length) from = l[0].path;
		});
	});

	// the folder as Completist sees it, and as the device that plays the games sees it
	let from = $state('');
	let to = $state('');
	const retroarchUrl = $derived(`/api/export/retroarch?${new URLSearchParams({ from, to })}`);
	const example = $derived(to ? `${to.replace(/[\\/]+$/, '')}${to.includes('\\') ? '\\' : '/'}snes${to.includes('\\') ? '\\' : '/'}Super Metroid (Japan, USA) (En,Ja).zip` : '');
</script>

<p class="max-w-prose text-mute">
	{t('Take your collection to the programs that play it. Only the verified files are exported, with the names of the DATs, so the box art shows in them too.')}
</p>

<div class="mt-8 grid max-w-4xl gap-6 lg:grid-cols-2">
	<section class="panel flex flex-col p-5" aria-labelledby="ra">
		<h3 id="ra" class="t-section">RetroArch</h3>
		<p class="mt-2 text-sm text-mute">
			{t('A playlist for each system, named like its database so RetroArch finds the thumbnails. Copy the files to the playlists folder of RetroArch.')}
		</p>
		<div class="mt-5 space-y-4">
			<label class="block">
				<span class="text-sm text-mute">{t('The folder of your ROMs in Completist')}</span>
				<input class="field mt-1" bind:value={from} list="export-libraries" autocomplete="off" />
				<datalist id="export-libraries">
					{#each libraries as l (l.id)}<option value={l.path}></option>{/each}
				</datalist>
			</label>
			<label class="block">
				<span class="text-sm text-mute">{t('The same folder on the device with RetroArch')}</span>
				<input class="field mt-1" bind:value={to} placeholder="/storage/roms, C:\Games\ROMs..." autocomplete="off" />
				<span class="mt-1 block text-xs text-faint">{to ? t('For example: {p}', { p: example }) : t('Empty: the paths stay as Completist sees them.')}</span>
			</label>
		</div>
		<a class="btn btn-primary mt-6 self-start" href={retroarchUrl} download><Icon name="download" />{t('Download the playlists')}</a>
	</section>

	<section class="panel flex flex-col p-5" aria-labelledby="esde">
		<h3 id="esde" class="t-section">ES-DE</h3>
		<p class="mt-2 text-sm text-mute">
			{t('A gamelist.xml for each system, with the names of the games without region tags. The paths are relative to the folder of each system (snes, megadrive...), as ES-DE expects. Copy the folders inside gamelists to the gamelists folder of ES-DE.')}
		</p>
		<p class="mt-3 text-sm text-mute">{t('ES-DE reads its own gamelists too: back them up before replacing them.')}</p>
		<a class="btn btn-primary mt-auto self-start" href="/api/export/esde" download><Icon name="download" />{t('Download the gamelists')}</a>
	</section>
</div>
