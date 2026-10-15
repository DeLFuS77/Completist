// The theme of this device: the system's own, or one chosen in the settings.

export type Theme = 'auto' | 'dark' | 'light';

const key = 'completist.theme';

function saved(): Theme {
	try {
		const v = localStorage.getItem(key);
		if (v === 'dark' || v === 'light') return v;
	} catch {
		/* storage may be blocked */
	}
	return 'auto';
}

export const theme = $state({ value: saved(), light: false });

const media = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: light)') : null;

function apply() {
	theme.light = theme.value === 'light' || (theme.value === 'auto' && !!media?.matches);
	const root = document.documentElement;
	if (theme.value === 'auto') delete root.dataset.theme;
	else root.dataset.theme = theme.value;
}

export function setTheme(value: Theme) {
	theme.value = value;
	media?.addEventListener('change', apply);
apply();
	try {
		if (value === 'auto') localStorage.removeItem(key);
		else localStorage.setItem(key, value);
	} catch {
		/* storage may be blocked */
	}
}

apply();
