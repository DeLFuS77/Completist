// Translations. The English text is the key, so messages from the server (English sentences)
// are translated the same way. {name} placeholders are filled from the second argument.

import { es } from './locales/es';

export const languages = [
	{ code: 'en', name: 'English' },
	{ code: 'es', name: 'Español' }
] as const;

const dictionaries: Record<string, Record<string, string>> = { es };

function detect(): string {
	try {
		const saved = localStorage.getItem('completist.language');
		if (saved && (saved === 'en' || dictionaries[saved])) return saved;
	} catch {
		/* storage may be blocked */
	}
	const nav = (navigator.language || 'en').slice(0, 2).toLowerCase();
	return dictionaries[nav] ? nav : 'en';
}

export const locale = $state({ code: detect() });

export function setLanguage(code: string) {
	locale.code = code === 'en' || dictionaries[code] ? code : 'en';
	document.documentElement.lang = locale.code;
	try {
		localStorage.setItem('completist.language', locale.code);
	} catch {
		/* storage may be blocked */
	}
}

export function t(text: string, values?: Record<string, string | number>): string {
	const dict = dictionaries[locale.code];
	let out = (dict && dict[text]) || text;
	if (values) {
		for (const [k, v] of Object.entries(values)) out = out.replaceAll(`{${k}}`, String(v));
	}
	return out;
}

/** a number in the language of the interface */
export function n(value: number, digits = 0): string {
	return new Intl.NumberFormat(locale.code, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(value);
}

/** one of two texts by count: plural('{n} game', '{n} games', 3) */
export function plural(one: string, many: string, count: number): string {
	return t(count === 1 ? one : many, { n: n(count) });
}

export function bytes(value: number): string {
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let v = value;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${n(v, v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

export function date(value: string | number | Date): string {
	const d = new Date(value);
	if (isNaN(d.getTime())) return '';
	return new Intl.DateTimeFormat(locale.code, { dateStyle: 'medium', timeStyle: 'short' }).format(d);
}

/** "3 minutes ago", "in 2 hours" */
export function relative(value: string | number | Date): string {
	const d = new Date(value).getTime();
	const diff = (d - Date.now()) / 1000;
	const rtf = new Intl.RelativeTimeFormat(locale.code, { numeric: 'auto' });
	const abs = Math.abs(diff);
	if (abs < 60) return rtf.format(Math.round(diff), 'second');
	if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
	if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
	return rtf.format(Math.round(diff / 86400), 'day');
}
