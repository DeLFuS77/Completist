// A small router on the history API: the Go server answers every path with the interface.

export type Route =
	| { name: 'home' }
	| { name: 'system'; id: string }
	| { name: 'game'; id: number }
	| { name: 'tasks' }
	| { name: 'settings'; tab: string }
	| { name: 'notfound' };

function parse(pathname: string): Route {
	const parts = pathname.split('/').filter(Boolean).map(decodeURIComponent);
	if (parts.length === 0) return { name: 'home' };
	if (parts[0] === 'systems' && parts[1]) return { name: 'system', id: parts[1] };
	if (parts[0] === 'games' && parts[1] && /^\d+$/.test(parts[1])) return { name: 'game', id: Number(parts[1]) };
	if (parts[0] === 'tasks') return { name: 'tasks' };
	if (parts[0] === 'settings') return { name: 'settings', tab: parts[1] ?? 'libraries' };
	return { name: 'notfound' };
}

export const router = $state({ route: parse(location.pathname), search: new URLSearchParams(location.search) });

function update() {
	router.route = parse(location.pathname);
	router.search = new URLSearchParams(location.search);
}

export function navigate(to: string, replace = false) {
	if (to === location.pathname + location.search) return;
	if (replace) history.replaceState({}, '', to);
	else history.pushState({}, '', to);
	update();
	if (!replace) window.scrollTo({ top: 0 });
}

/** changes one query parameter without a new history entry */
export function setParam(name: string, value: string | null) {
	const params = new URLSearchParams(location.search);
	if (value === null || value === '') params.delete(name);
	else params.set(name, value);
	const qs = params.toString();
	history.replaceState({}, '', location.pathname + (qs ? `?${qs}` : ''));
	router.search = params;
}

export function startRouter() {
	addEventListener('popstate', update);
	// links inside the app change the page without a reload
	document.addEventListener('click', (e) => {
		if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
		const a = (e.target as Element).closest?.('a');
		if (!a || a.target || a.hasAttribute('download')) return;
		const href = a.getAttribute('href');
		if (!href || !href.startsWith('/') || href.startsWith('//') || href.startsWith('/api/')) return;
		e.preventDefault();
		navigate(href);
	});
}
