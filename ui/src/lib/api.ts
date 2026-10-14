// The JSON API of the Go server.

export class ApiError extends Error {
	status: number;
	data: Record<string, unknown>;
	constructor(status: number, message: string, data: Record<string, unknown> = {}) {
		super(message);
		this.status = status;
		this.data = data;
	}
}

type Listener = () => void;
const unauthorizedListeners = new Set<Listener>();

/** called when a request needs a login, so the app can show the login form */
export function onUnauthorized(fn: Listener) {
	unauthorizedListeners.add(fn);
	return () => unauthorizedListeners.delete(fn);
}

export async function request<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
	const init: RequestInit = { method, headers: { 'X-Requested-With': 'completist' } as Record<string, string>, credentials: 'same-origin' };
	if (body instanceof FormData || body instanceof Blob || typeof body === 'string') {
		init.body = body as BodyInit;
	} else if (body !== undefined) {
		(init.headers as Record<string, string>)['Content-Type'] = 'application/json';
		init.body = JSON.stringify(body);
	}
	const res = await fetch(path, init);
	const text = await res.text();
	let data: any = {};
	try {
		data = text ? JSON.parse(text) : {};
	} catch {
		data = { error: text };
	}
	if (!res.ok) {
		if (res.status === 401 && !path.startsWith('/api/auth/')) unauthorizedListeners.forEach((fn) => fn());
		throw new ApiError(res.status, data?.error ?? res.statusText, data);
	}
	return data as T;
}

export const get = <T>(path: string) => request<T>('GET', path);
export const post = <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {});
export const put = <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {});
export const del = <T>(path: string) => request<T>('DELETE', path);

// ---- types of the API ----

export interface System {
	id: string;
	name: string;
	short: string;
	maker: string;
	year: number;
	plugin: string;
	libretro: string;
	extensions: string[];
}

export interface FileCounts {
	files: number;
	ok: number;
	bad: number;
	unknown: number;
	noDat: number;
	errors: number;
	duplicates: number;
	bytes: number;
}

export type Mode = 'full' | 'official' | '1g1r';

export interface Progress {
	system: System;
	mode: Mode;
	hasDat: boolean;
	total: number;
	have: number;
	missing: number;
	percent: number;
	files: FileCounts;
}

export interface Summary {
	systems: number;
	total: number;
	have: number;
	missing: number;
	percent: number;
	files: number;
	bytes: number;
	bad: number;
	unknown: number;
	duplicates: number;
}

export interface Overview {
	summary: Summary;
	systems: Progress[];
	libraries: number;
	dats: number;
	scanning: boolean;
	version: string;
}

export interface Entry {
	id: number;
	name: string;
	title: string;
	regions: string[];
	flags: string[];
	owned: boolean;
	files: number;
	versions: number;
	wanted: boolean;
	discs?: number;
	discsOwned?: number;
}

export interface GameList {
	mode: Mode;
	total: number;
	items: Entry[];
	regions: Record<string, number>;
}

export interface GameFile {
	id: number;
	libraryId: number;
	path: string;
	entry?: string;
	size: number;
	mtime: number;
	system: string;
	crc: string;
	md5: string;
	sha1: string;
	altForm?: string;
	status: 'ok' | 'bad' | 'unknown' | 'nodat' | 'error';
	error?: string;
	gameId?: number;
}

export interface GameDetail {
	id: number;
	name: string;
	title: string;
	system: System;
	regions: string[];
	languages: string[];
	flags: string[];
	revision: number;
	owned: boolean;
	files: GameFile[];
	roms: { name: string; size: number; crc: string; sha1: string }[];
	versions: { id: number; name: string; regions: string[]; flags: string[]; owned: boolean }[];
	wanted: boolean;
}

export interface Wish {
	system: string;
	name: string;
	added: string;
	id: number;
	title: string;
	regions: string[];
	owned: boolean;
	inDat: boolean;
}

export interface Recent {
	id: number;
	system: string;
	name: string;
	title: string;
	since: string;
}

export interface StatGroup {
	name: string;
	systems?: number;
	have: number;
	total: number;
	percent: number;
}

export interface Stats {
	summary: Summary;
	history: { day: string; have: number; total: number; percent: number; bytes: number }[];
	makers: StatGroup[];
	regions: StatGroup[];
	sizes: { system: string; files: number; bytes: number }[];
	closest: { system: string; have: number; missing: number; percent: number }[];
	recent: Recent[];
	wishlist: { total: number; owned: number };
}

export interface Library {
	id: number;
	path: string;
	name: string;
	readOnly: boolean;
	created: string;
	files: number;
}

export interface DatInfo {
	system: string;
	name: string;
	version: string;
	source: string;
	imported: string;
	games: number;
}

export interface SystemSetting extends System {
	mode: Mode;
	dat: DatInfo | null;
}

export interface Preferences {
	defaultMode: Mode;
	modes: Record<string, Mode>;
	regions: string[];
}

export interface TaskNote {
	text: string;
	detail?: string;
}

export interface Task {
	id: number;
	kind: 'scan' | 'datsync' | 'datimport' | string;
	trigger: string;
	status: 'running' | 'success' | 'warning' | 'failed';
	started: string;
	finished?: string;
	current: number;
	total: number;
	message: string;
	error?: TaskNote;
	warnings?: TaskNote[];
	files?: number;
	games?: number;
}

export interface AuthStatus {
	enabled: boolean;
	user: string;
	isAdmin: boolean;
	fromEnv: boolean;
	language: string;
	weakEnvPassword: boolean;
	loggedIn: boolean;
}

export interface MosaicRow {
	id: string;
	total: number;
	/** base64, one bit per entry in name order: 1 when owned */
	bits: string;
}
