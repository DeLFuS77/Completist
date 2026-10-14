// Builds the interface into ../internal/webui/dist, which the Go binary embeds.
//
//   node build.mjs           one build, minified
//   node build.mjs --watch   rebuild on every change; run the app with
//                            COMPLETIST_UI_DIR=internal/webui/dist to see them without
//                            building Go again
//
// Svelte components are compiled with the Svelte compiler, the rest is bundled by esbuild,
// and the CSS goes through Tailwind. index.html has no inline script, so the content security
// policy needs no exception.
import * as esbuild from 'esbuild';
import { compile as compileSvelte } from 'svelte/compiler';
import { compile as compileTailwind } from 'tailwindcss';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const src = path.join(here, 'src');
const out = path.resolve(here, '../internal/webui/dist');
const watch = process.argv.includes('--watch');
const require = createRequire(import.meta.url);

/** esbuild plugin: compiles .svelte files, with their styles injected by the component */
const sveltePlugin = {
	name: 'svelte',
	setup(build) {
		build.onLoad({ filter: /\.svelte$/ }, async (args) => {
			const source = await fs.readFile(args.path, 'utf8');
			const filename = path.relative(here, args.path);
			try {
				const { js, warnings } = compileSvelte(source, {
					filename,
					generate: 'client',
					css: 'injected',
					dev: watch
				});
				return {
					contents: js.code,
					loader: 'js',
					resolveDir: path.dirname(args.path),
					warnings: warnings.map((w) => ({ text: w.message, location: w.start && { file: filename, line: w.start.line, column: w.start.column } }))
				};
			} catch (e) {
				return { errors: [{ text: e.message, location: e.start && { file: filename, line: e.start.line, column: e.start.column } }] };
			}
		});
		// .svelte.ts modules use runes outside components
		build.onLoad({ filter: /\.svelte\.ts$/ }, async (args) => {
			const ts = await fs.readFile(args.path, 'utf8');
			const { code } = await esbuild.transform(ts, { loader: 'ts' });
			const { compileModule } = await import('svelte/compiler');
			const { js } = compileModule(code, { filename: path.relative(here, args.path), generate: 'client', dev: watch });
			return { contents: js.code, loader: 'js', resolveDir: path.dirname(args.path) };
		});
	}
};

/** the class names used in the sources, for Tailwind to generate */
async function candidates() {
	const found = new Set();
	const walk = async (dir) => {
		for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
			const p = path.join(dir, entry.name);
			if (entry.isDirectory()) await walk(p);
			else if (/\.(svelte|ts|html)$/.test(entry.name)) {
				const text = await fs.readFile(p, 'utf8');
				for (const token of text.split(/[\s"'`{}<>=;]+/)) {
					if (token && token.length < 120) found.add(token.replace(/^class:/, ''));
				}
			}
		}
	};
	await walk(src);
	return [...found];
}

async function buildCss() {
	const entry = path.join(src, 'app.css');
	const css = await fs.readFile(entry, 'utf8');
	const tailwindDir = path.dirname(require.resolve('tailwindcss/package.json'));
	const compiler = await compileTailwind(css, {
		base: src,
		loadStylesheet: async (id, base) => {
			const file = id === 'tailwindcss' ? path.join(tailwindDir, 'index.css') : id.startsWith('tailwindcss/') ? path.join(tailwindDir, id.slice('tailwindcss/'.length)) : path.resolve(base, id);
			return { path: file, base: path.dirname(file), content: await fs.readFile(file, 'utf8') };
		}
	});
	const built = compiler.build(await candidates());
	const { code } = await esbuild.transform(built, { loader: 'css', minify: !watch });
	return code;
}

const hash = (text) => createHash('sha256').update(text).digest('hex').slice(0, 10);

async function writeIndex(jsName, cssName) {
	const template = await fs.readFile(path.join(src, 'index.html'), 'utf8');
	const html = template
		.replace('%css%', `<link rel="stylesheet" href="/${cssName}">`)
		.replace('%js%', `<script type="module" src="/${jsName}"></script>`);
	await fs.writeFile(path.join(out, 'index.html'), html);
}

async function copyStatic() {
	const from = path.join(here, 'static');
	await fs.cp(from, out, { recursive: true });
}

async function buildOnce(ctx) {
	const started = Date.now();
	const result = ctx ? await ctx.rebuild() : await esbuild.build(jsOptions);
	if (result.errors.length) return false;
	const jsFile = Object.keys(result.metafile.outputs).find((f) => f.endsWith('.js'));
	const css = await buildCss();
	const cssName = `assets/app-${hash(css)}.css`;
	await fs.writeFile(path.join(out, cssName), css);
	await writeIndex(path.relative(out, path.resolve(jsFile)).split(path.sep).join('/'), cssName);
	await copyStatic();
	console.log(`built in ${Date.now() - started} ms`);
	return true;
}

const jsOptions = {
	entryPoints: [path.join(src, 'main.ts')],
	bundle: true,
	format: 'esm',
	splitting: false,
	outdir: path.join(out, 'assets'),
	entryNames: '[name]-[hash]',
	minify: !watch,
	sourcemap: watch ? 'inline' : false,
	target: ['es2022'],
	conditions: ['svelte', 'browser', watch ? 'development' : 'production'],
	mainFields: ['svelte', 'browser', 'module', 'main'],
	plugins: [sveltePlugin],
	metafile: true,
	logLevel: 'warning',
	define: { 'process.env.NODE_ENV': watch ? '"development"' : '"production"' }
};

await fs.rm(out, { recursive: true, force: true });
await fs.mkdir(path.join(out, 'assets'), { recursive: true });
// keeps the folder in git, so Go can build before the interface is
await fs.writeFile(path.join(out, '.gitkeep'), '');

if (!watch) {
	if (!(await buildOnce())) process.exit(1);
} else {
	const ctx = await esbuild.context(jsOptions);
	await buildOnce(ctx);
	let timer;
	const { watch: fsWatch } = await import('node:fs');
	fsWatch(here, { recursive: true }, (_, file) => {
		if (!file || file.startsWith('node_modules') || !/^(src|static)/.test(file)) return;
		clearTimeout(timer);
		timer = setTimeout(async () => {
			await fs.rm(path.join(out, 'assets'), { recursive: true, force: true });
			await fs.mkdir(path.join(out, 'assets'), { recursive: true });
			buildOnce(ctx).catch((e) => console.error(e));
		}, 100);
	});
	console.log('watching for changes');
}
