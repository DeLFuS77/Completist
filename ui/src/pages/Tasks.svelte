<script lang="ts">
	import { del, type Task } from '../lib/api';
	import { live, session } from '../lib/state.svelte';
	import { t, n, date, relative } from '../lib/i18n.svelte';
	import Bar from '../components/Bar.svelte';
	import Icon from '../components/Icon.svelte';

	const kindName: Record<string, string> = { scan: 'Library scan', datsync: 'DAT download', datimport: 'DAT import' };
	const triggerName: Record<string, string> = {
		manual: 'started by you',
		schedule: 'scheduled',
		watcher: 'after a change in the folders',
		startup: 'at start',
		settings: 'after a change in the settings'
	};
	const isAdmin = $derived(session.status?.isAdmin ?? false);

	function result(task: Task): string {
		if (task.kind === 'scan') return t('{f} files, {r} read now', { f: n(task.files ?? 0), r: n(task.games ?? 0) });
		if (task.kind === 'datsync') return t('{d} DATs, {g} games', { d: n(task.files ?? 0), g: n(task.games ?? 0) });
		return '';
	}

	// the elapsed time of running tasks moves
	let now = $state(Date.now());
	$effect(() => {
		const i = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(i);
	});
	function elapsed(task: Task) {
		const end = task.finished ? new Date(task.finished).getTime() : now;
		const s = Math.max(0, Math.round((end - new Date(task.started).getTime()) / 1000));
		return s < 60 ? `${s} s` : `${Math.floor(s / 60)} min ${s % 60} s`;
	}
</script>

<div class="flex flex-wrap items-baseline justify-between gap-4">
	<h1 class="t-title">{t('Tasks')}</h1>
	{#if isAdmin && live.tasks.some((t) => t.status !== 'running')}
		<button class="btn btn-quiet" onclick={() => del('/api/tasks')}>{t('Clear the finished tasks')}</button>
	{/if}
</div>

{#if live.tasks.length === 0}
	<p class="mt-8 text-mute">{t('No task has run yet. Scans and DAT downloads show up here while they run.')}</p>
{:else}
	<ul class="mt-8 space-y-3">
		{#each live.tasks as task (task.id)}
			<li class="panel p-5">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div>
						<h2 class="font-semibold">{t(kindName[task.kind] ?? task.kind)}</h2>
						<p class="text-sm text-mute">
							{t(triggerName[task.trigger] ?? task.trigger)}, <time datetime={task.started} title={date(task.started)}>{relative(task.started)}</time>, {elapsed(task)}
						</p>
					</div>
					<span
						class="chip !py-1 text-xs"
						class:!text-gold={task.status === 'running'}
						class:!text-good={task.status === 'success'}
						class:!text-warn={task.status === 'warning'}
						class:!text-bad={task.status === 'failed'}
					>
						{task.status === 'running' ? t('Running') : task.status === 'success' ? t('Done') : task.status === 'warning' ? t('Done with warnings') : t('Failed')}
					</span>
				</div>
				{#if task.status === 'running'}
					<div class="mt-4">
						{#if task.total > 0}
							<Bar value={task.current} total={task.total} color="var(--color-gold)" label={t(kindName[task.kind] ?? task.kind)} />
							<div class="mt-2 flex justify-between gap-4 text-sm text-mute">
								<span class="truncate">{t(task.message)}</span>
								<span class="t-num shrink-0">{n(task.current)} / {n(task.total)}</span>
							</div>
						{:else}
							<div class="h-2 overflow-hidden rounded-full bg-line/70"><div class="indeterminate h-full w-1/3 rounded-full bg-gold"></div></div>
							<div class="mt-2 truncate text-sm text-mute">{t(task.message)}</div>
						{/if}
					</div>
				{:else if result(task)}
					<p class="mt-3 text-sm text-mute">{result(task)}</p>
				{/if}
				{#if task.error}
					<p class="mt-3 text-sm text-bad">{t(task.error.text)} {#if task.error.detail}<span class="text-mute">{task.error.detail}</span>{/if}</p>
				{/if}
				{#if task.warnings?.length}
					<details class="mt-3 text-sm">
						<summary class="cursor-pointer text-warn">{t('{n} warnings', { n: n(task.warnings.length) })}</summary>
						<ul class="mt-2 space-y-1 text-mute">
							{#each task.warnings as w}<li><span class="text-ink">{t(w.text)}</span> {w.detail}</li>{/each}
						</ul>
					</details>
				{/if}
				{#if isAdmin && task.status === 'failed'}
					<button class="mt-3 inline-flex items-center gap-1 text-sm text-mute hover:text-ink" onclick={() => del(`/api/tasks/${task.id}`)}><Icon name="close" size={14} />{t('Dismiss')}</button>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

<style>
	.indeterminate {
		animation: slide 1.4s var(--ease-out-soft) infinite;
	}
	@keyframes slide {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(300%);
		}
	}
</style>
