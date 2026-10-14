<script lang="ts">
	import { post } from '../../lib/api';
	import { session, loadSession } from '../../lib/state.svelte';
	import { t, languages, locale, setLanguage } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	let current = $state('');
	let password = $state('');
	let message = $state('');
	let error = $state('');

	async function changeLanguage(code: string) {
		setLanguage(code);
		if (session.status?.loggedIn && session.status.user) await post('/api/account/language', { language: code }).catch(() => {});
	}
	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		error = message = '';
		try {
			await post('/api/account/password', { current, password });
			current = password = '';
			message = t('Your password was changed. Your other sessions have ended.');
		} catch (e: any) {
			error = t(e.message);
		}
	}
	async function logout() {
		await post('/api/auth/logout');
		await loadSession();
	}
</script>

<section>
	<h3 class="t-section">{t('Language')}</h3>
	<div class="mt-4 flex gap-2" role="group" aria-label={t('Language')}>
		{#each languages as l}
			<button class="chip" aria-pressed={locale.code === l.code} onclick={() => changeLanguage(l.code)}>{l.name}</button>
		{/each}
	</div>
</section>

{#if session.status?.enabled && session.status.user && !session.status.fromEnv}
	<form class="panel mt-10 max-w-md space-y-4 p-5" onsubmit={changePassword}>
		<h3 class="t-section">{t('Change your password')}</h3>
		<label class="block">
			<span class="text-sm text-mute">{t('Current password')}</span>
			<input class="field mt-1" type="password" bind:value={current} required autocomplete="current-password" />
		</label>
		<label class="block">
			<span class="text-sm text-mute">{t('New password')}</span>
			<input class="field mt-1" type="password" bind:value={password} required minlength="10" autocomplete="new-password" />
		</label>
		{#if message}<p class="text-sm text-good">{message}</p>{/if}
		{#if error}<p class="text-sm text-bad">{error}</p>{/if}
		<button class="btn btn-primary" type="submit">{t('Change the password')}</button>
	</form>
{/if}

{#if session.status?.enabled && session.status.user}
	<button class="btn btn-quiet mt-10" onclick={logout}><Icon name="logout" />{t('Log out')}</button>
{/if}
