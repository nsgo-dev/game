// The repository's files, for pages that show them. Pages are bundled before they render, so their own address says nothing of where the repository is; Astro runs in the site's folder, apps/website.
import { readFileSync } from 'node:fs';
import path from 'node:path';

export const REPO_ROOT = path.resolve(process.cwd(), '../..');

export const repoFile = (file: string) => readFileSync(path.join(REPO_ROOT, file), 'utf8');
