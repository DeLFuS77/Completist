<script lang="ts">
	import { ApiError, get, post, put } from '../../lib/api';
	import { t } from '../../lib/i18n.svelte';

	type Options = {
		discordWebhookUrl: string;
		telegramBotToken: string;
		telegramChatId: string;
		webhookUrl: string;
		language: string;
		newGames: boolean;
		completeSets: boolean;
		damaged: boolean;
		datChanges: boolean;
	};
	let o = $state<Options | null>(null);
	let errors = $state<Record<string, string>>({});
	let message = $state('');
	let error = $state('');
	let busy = $state(false);
	$effect(() => {
		get<Options>('/api/notifications').then((x) => (o = x));
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		message = error = '';
		busy = true;
		try {
			o = await put<Options>('/api/notifications', o);
			message = t('Saved');
		} catch (err) {
			const ae = err as ApiError;
			const fields = (ae.data?.fields as { field: string; message: string }[]) ?? [];
			errors = Object.fromEntries(fields.map((f) => [f.field, t(f.message)]));
			if (!fields.length) error = t(ae.message);
		} finally {
			busy = false;
		}
	}
	async function test() {
		message = error = '';
		try {
			await post('/api/notifications/test');
			message = t('Sent. Check your channels.');
		} catch (err) {
			error = t((err as Error).message);
		}
	}
	const events: { key: keyof Options; name: string; text: string }[] = [
		{ key: 'newGames', name: 'New games in your collection', text: 'After a scan, the games that were not there before.' },
		{ key: 'completeSets', name: 'Complete sets', text: 'When nothing is missing any more in a system.' },
		{ key: 'damaged', name: 'Damaged or modified files', text: 'Files with the name of a game but other content.' },
		{ key: 'datChanges', name: 'New entries in the DATs', text: 'When the weekly update adds games to a DAT, so there is more to collect.' }
	];
</script>

<p class="max-w-prose text-mute">
	{t('Completist can tell you about changes in your collection through Discord, Telegram or any web address that accepts JSON (ntfy, Home Assistant, n8n...). Only what is new since the last scan is sent.')}
</p>

{#if o}
	<form class="mt-6 max-w-2xl space-y-8" onsubmit={save}>
		<section class="panel space-y-4 p-5">
			<h3 class="t-section">{t('Channels')}</h3>
			<label class="block">
				<span class="text-sm text-mute">{t('Discord webhook')}</span>
				<input class="field mt-1" bind:value={o.discordWebhookUrl} placeholder="https://discord.com/api/webhooks/..." autocomplete="off" />
				{#if errors.discordWebhookUrl}<span class="mt-1 block text-sm text-bad">{errors.discordWebhookUrl}</span>{/if}
			</label>
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span class="text-sm text-mute">{t('Telegram bot token')}</span>
					<input class="field mt-1" type="password" bind:value={o.telegramBotToken} autocomplete="off" />
					{#if errors.telegramBotToken}<span class="mt-1 block text-sm text-bad">{errors.telegramBotToken}</span>{/if}
				</label>
				<label class="block">
					<span class="text-sm text-mute">{t('Telegram chat ID')}</span>
					<input class="field mt-1" bind:value={o.telegramChatId} placeholder="123456789" autocomplete="off" />
					{#if errors.telegramChatId}<span class="mt-1 block text-sm text-bad">{errors.telegramChatId}</span>{/if}
				</label>
			</div>
			<label class="block">
				<span class="text-sm text-mute">{t('Webhook (JSON)')}</span>
				<input class="field mt-1" bind:value={o.webhookUrl} placeholder="https://..." autocomplete="off" />
				{#if errors.webhookUrl}<span class="mt-1 block text-sm text-bad">{errors.webhookUrl}</span>{/if}
			</label>
			<label class="block max-w-xs">
				<span class="text-sm text-mute">{t('Language of the messages')}</span>
				<select class="field mt-1" bind:value={o.language}>
					<option value="en">English</option>
					<option value="es">Español</option>
				</select>
			</label>
		</section>

		<section>
			<h3 class="t-section">{t('What to send')}</h3>
			<ul class="mt-4 space-y-3">
				{#each events as ev (ev.key)}
					<li>
						<label class="flex items-start gap-3">
							<input type="checkbox" bind:checked={o[ev.key] as boolean} class="mt-1 accent-[var(--color-gold)]" />
							<span>{t(ev.name)}<span class="block text-sm text-mute">{t(ev.text)}</span></span>
						</label>
					</li>
				{/each}
			</ul>
		</section>

		{#if message}<p class="text-sm text-good" role="status">{message}</p>{/if}
		{#if error}<p class="text-sm text-bad" role="alert">{error}</p>{/if}
		<div class="flex flex-wrap gap-3">
			<button class="btn btn-primary" type="submit" disabled={busy}>{t('Save the notifications')}</button>
			<button class="btn btn-quiet" type="button" onclick={test}>{t('Send a test message')}</button>
		</div>
	</form>
{/if}
