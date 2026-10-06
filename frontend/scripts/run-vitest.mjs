#!/usr/bin/env node

import { realpathSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

// Native filesystem spelling gives Vite one drive/path identity, including
// invocations through a differently cased drive letter or a junction.
const root = realpathSync.native(fileURLToPath(new URL('../',import.meta.url)))
const result = spawnSync(process.execPath,[path.join(root,'node_modules/vitest/vitest.mjs'),...process.argv.slice(2)],{cwd:root,stdio:'inherit'})
if (result.error) throw result.error
process.exitCode = result.status ?? 1
