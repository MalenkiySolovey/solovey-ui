// Canonical Frontend helpers are loaded only from the admitted immutable source archive.
import path from 'node:path'
import {pathToFileURL} from 'node:url'
import {resolveFrontendSource} from './frontend-artifacts.mjs'
export const frontendSourceDirectory = await resolveFrontendSource()
export const frontendFixtureDirectory = () => resolveFrontendSource({fixture: true})
const integrity = await import(pathToFileURL(path.join(frontendSourceDirectory,'scripts/component-source-integrity.mjs')).href)
const inputs = await import(pathToFileURL(path.join(frontendSourceDirectory,'scripts/component-build-inputs.mjs')).href)
export const {digest, frontendDigest, safeSourcePath, validateSourceLock, verifySourceFiles, regularSourceTree, verifyUIAPIInputs} = integrity
export const {buildInputSchema, frontendAPIFiles, frontendProjectionFiles, readFrontendBuildInputs, validateFrontendBuildInputs, selectFrontendEntries} = inputs
