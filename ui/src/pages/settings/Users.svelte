<script lang="ts">
	import { del, get, post, put } from '../../lib/api';
	import { session, loadSession } from '../../lib/state.svelte';
	import { t, date } from '../../lib/i18n.svelte';
	import Icon from '../../components/Icon.svelte';

	type User = { name: string; role: 'admin' | 'viewer'; language: string; created: string };
	let users = $state<User[]>([]);
	let envUser = $state('');
	let error = $state('');
	const enabled = $derived(session.status?.enabled ?? false);
	const load = () =>
		get<{ users: User[]; envUser: string }>('/api/users')
			.then((r) => {
				users = r.users;
				envUser = r.envUser;
			})
			.catch((e) => (error = t(e.message)));
	$effect(() => {
		load();
	});

	let name = $state('');
	let password = $state('');
	let role = $state<'admin' | 'viewer'>('viewer');
	async function add(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			await post('/api/users', { name, password, role: enabled ? role : 'admin' });
			name = password = '';
			await loadSession();
			load();
		} catch (e: any) {
			error = t(e.message);
		}
	}
	async function setRole(u: User, r: string) {
		error = '';
		try {
			await put(`/api/users/${encodeURIComponent(u.name)}`, { role: r });
			load();
		} catch (e: any) {
			error = t(e.message);
			load();
		}
	}
	async function remove(u: User) {
		if (!confirm(t('Delete the user {name}?', { name: u.name }))) return;
		error = '';
		try {
			await del(`/api/users/${encodeURIComponent(u.name)}`);
			load();
		} catch (e: any) {
			error = t(e.message);
		}
	}
</script>

{#if !enabled}
	<div class="panel max-w-2xl border border-warn/40 p-5">
		<h3 class="flex items-center gap-2 font-semibold text-warn"><Icon name="lock" />{t('Anyone on your network can use Completist')}</h3>
		<p class="mt-2 text-sm text-mute">
			{t('Create an administrator to require a login. From outside your local network the app does not answer until you do.')}
		</p>
	</div>
{:else}
	<p class="max-w-prose text-mute">{t('Administrators can change everything. Read-only users can browse the collection and see the tasks.')}</p>
{/if}

{#if users.length || envUser}
	<ul class="mt-6 max-w-2xl divide-y divide-line/60">
		{#if envUser}
			<li class="flex items-center gap-4 py-3">
				<Icon name="user" />
				<span class="flex-1 font-semibold">{envUser}</span>
				<span class="text-sm text-mute">{t('Administrator, set in the environment')}</span>
			</li>
		{/if}
		{#each users as u (u.name)}
			<li class="flex flex-wrap items-center gap-4 py-3">
				<Icon name="user" />
				<div class="min-w-0 flex-1">
					<div class="font-semibold">{u.name}{#if u.name === session.status?.user}<span class="font-normal text-mute"> ({t('you')})</span>{/if}</div>
					<div class="text-xs text-faint">{t('Created {d}', { d: date(u.created) })}</div>
				</div>
				<select class="field !w-auto !py-1.5 text-sm" value={u.role} onchange={(e) => setRole(u, e.currentTarget.value)} disabled={u.name === session.status?.user}>
					<option value="admin">{t('Administrator')}</option>
					<option value="viewer">{t('Read only')}</option>
				</select>
				<button class="rounded-md p-1 text-faint hover:text-bad disabled:opacity-30" onclick={() => remove(u)} disabled={u.name === session.status?.user} aria-label={t('Delete {name}', { name: u.name })}><Icon name="trash" size={16} /></button>
			</li>
		{/each}
	</ul>
{/if}

<form class="panel mt-6 max-w-2xl space-y-4 p-5" onsubmit={add}>
	<h3 class="t-section">{enabled ? t('Add a user') : t('Create the administrator')}</h3>
	<div class="grid gap-4 sm:grid-cols-2">
		<label class="block">
			<span class="text-sm text-mute">{t('User name')}</span>
			<input class="field mt-1" bind:value={name} required autocomplete="off" pattern="[A-Za-z0-9._\-]{'{'}1,32{'}'}" />
		</label>
		<label class="block">
			<span class="text-sm text-mute">{t('Password')}</span>
			<input class="field mt-1" type="password" bind:value={password} required minlength="10" autocomplete="new-password" />
		</label>
	</div>
	<p class="text-xs text-faint">{t('At least 10 characters. A short phrase is easier to remember than a jumble.')}</p>
	{#if enabled}
		<fieldset class="flex gap-6 text-sm">
			<legend class="sr-only">{t('Role')}</legend>
			<label class="flex items-center gap-2"><input type="radio" bind:group={role} value="viewer" class="accent-[var(--color-gold)]" />{t('Read only')}</label>
			<label class="flex items-center gap-2"><input type="radio" bind:group={role} value="admin" class="accent-[var(--color-gold)]" />{t('Administrator')}</label>
		</fieldset>
	{/if}
	{#if error}<p class="text-sm text-bad">{error}</p>{/if}
	<button class="btn btn-primary" type="submit">{enabled ? t('Add the user') : t('Create and log in')}</button>
</form>
