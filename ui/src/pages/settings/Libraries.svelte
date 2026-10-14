<script lang="ts">
	import { del, get, post, put, type Library } from '../../lib/api';
	import { collection, refreshCollection } from '../../lib/state.svelte';
	import { t, n, plural, bytes } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	let libraries = $state<Library[]>([]);
	let error = $state('');
	const load = () => get<Library[]>('/api/libraries').then((l) => (libraries = l));
	$effect(() => {
		collection.version;
		load();
	});

	// the images of the games the server keeps
	type Images = { images: number; bytes: number; mode: string; converting: boolean };
	let images = $state<Images | null>(null);
	let imagesTimer: ReturnType<typeof setTimeout>;
	const loadImages = () =>
		get<Images>('/api/covers').then((u) => {
			images = u;
			// while the box art is made smaller, the space goes down
			clearTimeout(imagesTimer);
			if (u.converting) imagesTimer = setTimeout(loadImages, 1500);
		});
	const imageModes = [
		{ id: 'original', name: 'Original', text: 'The PNG as libretro has it. About 200 KB a cover.' },
		{ id: 'high', name: 'High quality', text: 'A JPEG of quality 92 with the same pixels: no difference to see, about a quarter of the space. Recommended.' },
		{ id: 'small', name: 'Smallest', text: 'A JPEG of quality 80: about a sixth of the space. A close look shows some softness.' }
	];
	let imageMessage = $state('');
	async function setImageMode(mode: string) {
		imageMessage = '';
		try {
			images = await put<Images>('/api/covers', { mode });
			if (images.converting) {
				imageMessage = t('Making the stored box art smaller...');
				loadImages();
			} else imageMessage = t('Saved');
		} catch (e) {
			imageMessage = t((e as Error).message);
		}
	}
	$effect(() => {
		loadImages();
	});
	async function clearImages() {
		if (!confirm(t('Remove the stored images? They are downloaded again when a page shows them.'))) return;
		await del('/api/covers');
		loadImages();
	}

	// adding: a path typed or picked in the folder browser
	let adding = $state(false);
	let path = $state('');
	let name = $state('');
	let readOnly = $state(false);
	let browser = $state<{ path: string; parent: string; folders: string[] } | null>(null);
	let browseError = $state('');

	async function browse(to: string) {
		browseError = '';
		try {
			browser = await get(`/api/fs?path=${encodeURIComponent(to)}`);
			path = browser!.path;
		} catch (e: any) {
			browseError = t(e.message);
		}
	}
	function join(base: string, folder: string) {
		return base.endsWith('/') || base.endsWith('\\') ? base + folder : `${base}${base.includes('\\') && !base.includes('/') ? '\\' : '/'}${folder}`;
	}
	async function add(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			await post('/api/libraries', { path, name, readOnly });
			adding = false;
			path = name = '';
			readOnly = false;
			browser = null;
			await load();
			refreshCollection();
		} catch (e: any) {
			error = t(e.message);
		}
	}
	async function toggleReadOnly(lib: Library) {
		await put(`/api/libraries/${lib.id}`, { name: lib.name, readOnly: !lib.readOnly });
		load();
	}
	async function remove(lib: Library) {
		if (!confirm(t('Remove the library {name}? Its files stay on the disk; Completist only forgets them.', { name: lib.name }))) return;
		await del(`/api/libraries/${lib.id}`);
		await load();
		refreshCollection();
	}
</script>

<p class="max-w-prose text-mute">
	{t('A library is a folder with ROMs. Files in subfolders named after a system, such as snes or Mega Drive, are sorted by that name; the others by their extension. Zip files are read too.')}
</p>

<ul class="mt-6 space-y-3">
	{#each libraries as lib (lib.id)}
		<li class="panel flex flex-wrap items-center gap-4 p-4">
			<span class="grid h-10 w-10 place-items-center rounded-xl bg-raised text-mute"><Icon name="folder" /></span>
			<div class="min-w-0 flex-1">
				<div class="font-semibold">{lib.name}</div>
				<div class="break-all text-sm text-mute">{lib.path}</div>
			</div>
			<span class="text-sm text-mute">{plural('{n} file', '{n} files', lib.files)}</span>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" checked={lib.readOnly} onchange={() => toggleReadOnly(lib)} class="accent-[var(--color-gold)]" />
				{t('Read only')}
			</label>
			<button class="btn btn-danger !px-3" onclick={() => remove(lib)} aria-label={t('Remove {name}', { name: lib.name })}><Icon name="trash" size={16} /></button>
		</li>
	{:else}
		<li class="text-mute">{t('No library yet.')}</li>
	{/each}
</ul>

{#if !adding}
	<button class="btn btn-primary mt-6" onclick={() => { adding = true; browse(''); }}><Icon name="folder" />{t('Add a library')}</button>
{:else}
	<form class="panel mt-6 space-y-4 p-5" onsubmit={add}>
		<h3 class="t-section">{t('Add a library')}</h3>
		<label class="block">
			<span class="text-sm text-mute">{t('Folder')}</span>
			<input class="field mt-1" bind:value={path} required placeholder="/roms" />
		</label>
		{#if browser}
			<div class="rounded-xl border border-line">
				<div class="flex items-center gap-2 border-b border-line px-3 py-2 text-sm">
					<button type="button" class="rounded-md p-1 text-mute hover:bg-raised hover:text-ink disabled:opacity-40" onclick={() => browse(browser!.parent)} disabled={browser.parent === browser.path} aria-label={t('Parent folder')}><Icon name="up" size={16} /></button>
					<span class="truncate text-mute">{browser.path}</span>
				</div>
				<ul class="max-h-64 overflow-y-auto py-1">
					{#each browser.folders as f}
						<li><button type="button" class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-raised" onclick={() => browse(join(browser!.path, f))}><Icon name="folder" size={16} />{f}</button></li>
					{:else}
						<li class="px-3 py-2 text-sm text-mute">{t('No subfolders.')}</li>
					{/each}
				</ul>
			</div>
		{/if}
		{#if browseError}<p class="text-sm text-bad">{browseError}</p>{/if}
		<label class="block">
			<span class="text-sm text-mute">{t('Name (optional)')}</span>
			<input class="field mt-1" bind:value={name} placeholder={t('The name of the folder')} />
		</label>
		<label class="flex items-start gap-3 text-sm">
			<input type="checkbox" bind:checked={readOnly} class="mt-1 accent-[var(--color-gold)]" />
			<span>{t('Read only')}<span class="block text-mute">{t('Completist will never change anything in this folder. Useful for a share of another computer.')}</span></span>
		</label>
		{#if error}<p class="text-sm text-bad">{error}</p>{/if}
		<div class="flex gap-3">
			<button class="btn btn-primary" type="submit">{t('Add and scan')}</button>
			<button class="btn btn-quiet" type="button" onclick={() => { adding = false; browser = null; }}>{t('Cancel')}</button>
		</div>
	</form>
{/if}

<section class="mt-12 max-w-4xl" aria-labelledby="images">
	<h3 id="images" class="t-section">{t('Images')}</h3>
	<p class="mt-2 text-mute">
		{t('Box art, screenshots and title screens come from libretro-thumbnails. Completist downloads each one the first time a page shows it and keeps it, so it loads at once afterwards, also without internet.')}
	</p>
	{#if images}
		<h4 class="mt-6 font-semibold">{t('How to keep the box art')}</h4>
		<div class="mt-3 grid gap-3 md:grid-cols-3" role="radiogroup" aria-label={t('How to keep the box art')}>
			{#each imageModes as m (m.id)}
				<label class="panel flex cursor-pointer flex-col gap-2 border-2 p-4 transition-colors" class:border-gold={images.mode === m.id} class:border-transparent={images.mode !== m.id}>
					<span class="flex items-center gap-2 font-semibold">
						<input type="radio" name="image-mode" value={m.id} checked={images.mode === m.id} onchange={() => setImageMode(m.id)} class="accent-[var(--color-gold)]" />
						{t(m.name)}
					</span>
					<span class="text-sm text-mute">{t(m.text)}</span>
				</label>
			{/each}
		</div>
		<p class="mt-3 text-sm text-mute">
			{t('Screenshots and title screens are always kept as they come: they are small, and pixel art must stay sharp. Box art with transparent parts stays a PNG too.')}
			{#if images.mode !== 'original'}{' '}{t('To have the originals again, remove the stored images: they are downloaded again.')}{/if}
		</p>
		{#if imageMessage}<p class="mt-2 text-sm text-good" role="status">{imageMessage}</p>{/if}
		<div class="mt-5 flex flex-wrap items-center gap-4">
			<span>{plural('{n} image stored', '{n} images stored', images.images)}{images.images ? `, ${bytes(images.bytes)}` : ''}{images.converting ? ` · ${t('making them smaller...')}` : ''}</span>
			{#if images.images > 0}
				<button class="text-sm text-mute underline underline-offset-4 hover:text-ink" onclick={clearImages}>{t('Remove them')}</button>
			{/if}
		</div>
	{/if}
</section>
