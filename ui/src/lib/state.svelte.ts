// Shared state: who is logged in, the overview of the collection and the live tasks.

import { get, onUnauthorized, type AuthStatus, type Overview, type Task } from './api';
import { setLanguage } from './i18n.svelte';

export const session = $state<{ status: AuthStatus | null; needLogin: boolean }>({ status: null, needLogin: false });

export async function loadSession() {
	try {
		const status = await get<AuthStatus>('/api/auth/status');
		session.status = status;
		session.needLogin = status.enabled && !status.loggedIn;
		if (status.language) setLanguage(status.language);
	} catch {
		session.needLogin = false;
	}
}

onUnauthorized(() => {
	session.needLogin = true;
});

export const collection = $state<{ overview: Overview | null; error: string; version: number }>({ overview: null, error: '', version: 0 });

let loading: Promise<void> | null = null;

/** reloads the overview; pages that show the collection depend on collection.version */
export function refreshCollection(): Promise<void> {
	if (loading) return loading;
	loading = get<Overview>('/api/overview')
		.then((o) => {
			collection.overview = o;
			collection.error = '';
			collection.version++;
		})
		.catch((e) => {
			collection.error = e.message;
		})
		.finally(() => {
			loading = null;
		});
	return loading;
}

export const live = $state<{ tasks: Task[]; connected: boolean }>({ tasks: [], connected: false });

let source: EventSource | null = null;
const finished = new Set<number>();

/** follows the tasks as they run; the collection is reloaded when one finishes */
export function connectTasks() {
	source?.close();
	source = new EventSource('/api/tasks/events');
	source.onopen = () => (live.connected = true);
	source.onerror = () => {
		live.connected = false;
	};
	source.onmessage = (e) => {
		const data = JSON.parse(e.data) as { tasks: Task[] };
		let changed = false;
		for (const task of data.tasks) {
			if (task.status !== 'running' && !finished.has(task.id)) {
				finished.add(task.id);
				// tasks already finished when the page opened do not count
				if (live.tasks.some((t) => t.id === task.id && t.status === 'running')) changed = true;
			}
		}
		live.tasks = data.tasks;
		if (changed) refreshCollection();
	};
}

export function disconnectTasks() {
	source?.close();
	source = null;
}

export const running = () => live.tasks.filter((t) => t.status === 'running');
