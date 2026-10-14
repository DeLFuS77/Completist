<script lang="ts">
	import { ApiError, post } from '../lib/api';
	import { loadSession, refreshCollection, connectTasks } from '../lib/state.svelte';
	import { t } from '../lib/i18n.svelte';

	let name = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function login(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		busy = true;
		try {
			await post('/api/auth/login', { name, password });
			await loadSession();
			refreshCollection();
			connectTasks();
		} catch (e) {
			const err = e as ApiError;
			error = err.status === 429 ? t('Too many failed logins. Try again in {n} minutes.', { n: String(err.data.minutes ?? 15) }) : t(err.message);
		} finally {
			busy = false;
		}
	}
</script>

<main class="grid min-h-dvh place-items-center px-4">
	<form class="w-full max-w-sm" onsubmit={login}>
		<div class="font-display text-3xl font-bold tracking-tight">Completist</div>
		<p class="mt-2 text-mute">{t('Log in to see your collection.')}</p>
		<label class="mt-8 block">
			<span class="text-sm text-mute">{t('User name')}</span>
			<input class="field mt-1" bind:value={name} required autocomplete="username" />
		</label>
		<label class="mt-4 block">
			<span class="text-sm text-mute">{t('Password')}</span>
			<input class="field mt-1" type="password" bind:value={password} required autocomplete="current-password" />
		</label>
		{#if error}<p class="mt-4 text-sm text-bad" role="alert">{error}</p>{/if}
		<button class="btn btn-primary mt-6 w-full justify-center" type="submit" disabled={busy}>{t('Log in')}</button>
	</form>
</main>
