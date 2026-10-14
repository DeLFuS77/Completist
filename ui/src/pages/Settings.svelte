<script lang="ts">
	import { session } from '../lib/state.svelte';
	import { t } from '../lib/i18n.svelte';
	import Libraries from './settings/Libraries.svelte';
	import Dats from './settings/Dats.svelte';
	import Counting from './settings/Counting.svelte';
	import Users from './settings/Users.svelte';
	import Account from './settings/Account.svelte';
	import Notifications from './settings/Notifications.svelte';
	import Backups from './settings/Backups.svelte';

	let { tab }: { tab: string } = $props();
	const isAdmin = $derived(session.status?.isAdmin ?? false);
	const tabs = $derived(
		isAdmin
			? [
					{ id: 'libraries', name: t('Libraries') },
					{ id: 'dats', name: t('DATs') },
					{ id: 'counting', name: t('Counting') },
					{ id: 'notifications', name: t('Notifications') },
					{ id: 'backups', name: t('Backups') },
					{ id: 'users', name: t('Users') },
					{ id: 'account', name: t('Account') }
				]
			: [{ id: 'account', name: t('Account') }]
	);
	const current = $derived(tabs.some((x) => x.id === tab) ? tab : tabs[0].id);
</script>

<h1 class="t-title">{t('Settings')}</h1>
<nav class="mt-6 flex flex-wrap gap-2" aria-label={t('Settings')}>
	{#each tabs as x (x.id)}
		<a href="/settings/{x.id}" class="chip" aria-current={current === x.id ? 'page' : undefined}>{x.name}</a>
	{/each}
</nav>
<div class="mt-8">
	{#if current === 'libraries'}<Libraries />
	{:else if current === 'dats'}<Dats />
	{:else if current === 'counting'}<Counting />
	{:else if current === 'notifications'}<Notifications />
	{:else if current === 'backups'}<Backups />
	{:else if current === 'users'}<Users />
	{:else}<Account />{/if}
</div>
