const fs = require('fs');
const path = require('path');

let YAML;
try {
  YAML = require(path.join(process.cwd(), 'frontend', 'node_modules', 'yaml'));
} catch (error) {
  console.error(`ERROR: yaml parser is unavailable: ${error.message}`);
  process.exit(1);
}

const workflowDir = path.join(process.cwd(), '.github', 'workflows');
const sourceVersion = fs.readFileSync(path.join(process.cwd(), 'config', 'identity', 'version'), 'utf8').trim();
const releaseWorkflows = new Set(['docker.yml', 'release.yml', 'windows.yml']);
const files = fs
  .readdirSync(workflowDir)
  .filter((name) => /\.ya?ml$/.test(name))
  .sort();

for (const file of files) {
  const fullPath = path.join(workflowDir, file);
  const source = fs.readFileSync(fullPath, 'utf8');
  const workflow = YAML.parse(source);
  validateUses(workflow, file);
  validateRunInputs(workflow, file);
  if (releaseWorkflows.has(file)) {
    const defaultTag = workflow?.on?.workflow_dispatch?.inputs?.tag?.default;
    if (defaultTag !== `v${sourceVersion}`) {
      throw new Error(`${file}: workflow_dispatch tag default ${defaultTag ?? '<missing>'} does not match v${sourceVersion}`);
    }
    if (workflow?.on?.workflow_dispatch?.inputs?.preflight_only?.default !== true) {
      throw new Error(`${file}: manual workflow must default to non-publishing preflight_only mode`);
    }
  }
  console.log(`ok ${file}`);
}

validateReleaseContracts();

function validateUses(value, file, location = '$') {
  if (Array.isArray(value)) {
    value.forEach((item, index) => validateUses(item, file, `${location}[${index}]`));
    return;
  }
  if (!value || typeof value !== 'object') return;

  for (const [key, child] of Object.entries(value)) {
    const childLocation = `${location}.${key}`;
    if (key === 'uses' && typeof child === 'string') {
      if (child.startsWith('./')) {
        const actionPath = path.resolve(process.cwd(), child);
        if (!fs.existsSync(path.join(actionPath, 'action.yml')) && !fs.existsSync(path.join(actionPath, 'action.yaml'))) {
          throw new Error(`${file}:${childLocation}: local action is missing action.yml`);
        }
      } else if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+@[0-9a-f]{40}$/.test(child)) {
        throw new Error(`${file}:${childLocation}: action reference must use a full commit SHA: ${child}`);
      }
    }
    validateUses(child, file, childLocation);
  }
}

function validateRunInputs(value, file, location = '$') {
  if (Array.isArray(value)) {
    value.forEach((item, index) => validateRunInputs(item, file, `${location}[${index}]`));
    return;
  }
  if (!value || typeof value !== 'object') return;

  for (const [key, child] of Object.entries(value)) {
    const childLocation = `${location}.${key}`;
    if (key === 'run' && typeof child === 'string' && child.includes('${{ inputs.')) {
      throw new Error(`${file}:${childLocation}: workflow input must be passed through env, not interpolated into shell source`);
    }
    validateRunInputs(child, file, childLocation);
  }
}

function validateReleaseContracts() {
  const goMod = fs.readFileSync(path.join(process.cwd(), 'go.mod'), 'utf8');
  const goVersion = goMod.match(/^go\s+([0-9]+\.[0-9]+\.[0-9]+)\s*$/m)?.[1];
  if (!goVersion) throw new Error('go.mod: exact Go patch version is required');

  const dockerfile = fs.readFileSync(path.join(process.cwd(), 'Dockerfile'), 'utf8');
  const builder = dockerfile.match(
    /^FROM\s+--platform=\$TARGETPLATFORM\s+golang:([0-9]+\.[0-9]+\.[0-9]+)-alpine@sha256:([a-f0-9]{64})\s+AS\s+backend-builder\s*$/m,
  );
  if (!builder) throw new Error('Dockerfile: backend builder must pin an exact golang:<version>-alpine tag and sha256 digest');
  if (builder[1] !== goVersion) {
    throw new Error(`Dockerfile: Go ${builder[1]} does not match authoritative go.mod version ${goVersion}`);
  }

  const windowsPath = path.join(workflowDir, 'windows.yml');
  const windowsSource = fs.readFileSync(windowsPath, 'utf8');
  const windowsWorkflow = YAML.parse(windowsSource);
  const windowsTargets = windowsWorkflow?.jobs?.['build-windows']?.strategy?.matrix?.include;
  if (
    !Array.isArray(windowsTargets) ||
    windowsTargets.length !== 1 ||
    windowsTargets[0]?.arch !== 'amd64' ||
    windowsTargets[0]?.runner !== 'windows-latest' ||
    String(windowsTargets[0]?.cgo) !== '1'
  ) {
    throw new Error('windows.yml: shipped Windows target must be amd64 on windows-latest with CGO enabled');
  }
  if (/CGO_ENABLED\s*=\s*["']?0|\bNoCGO\b/.test(windowsSource)) {
    throw new Error('windows.yml: a non-CGO Windows release path is forbidden because SQLite would be nonfunctional');
  }
  if (!String(windowsWorkflow?.jobs?.['publish-windows']?.if ?? '').includes('preflight_only')) {
    throw new Error('windows.yml: publishing must be disabled in preflight_only mode');
  }
  for (const helper of ['windows/build-windows.ps1', 'windows/build-windows.bat']) {
    const source = fs.readFileSync(path.join(process.cwd(), helper), 'utf8');
    if (/CGO_ENABLED\s*=\s*["']?0|\bNoCGO\b/.test(source)) {
      throw new Error(`${helper}: a non-CGO Windows build fallback is forbidden`);
    }
  }

  const releasePath = path.join(workflowDir, 'release.yml');
  const releaseSource = fs.readFileSync(releasePath, 'utf8');
  const releaseWorkflow = YAML.parse(releaseSource);
  const preflight = releaseWorkflow?.jobs?.['release-preflight'];
  if (!preflight || !preflight.steps?.some((step) => typeof step.run === 'string' && step.run.includes('scripts/release-preflight.mjs'))) {
    throw new Error('release.yml: release-preflight job must execute scripts/release-preflight.mjs');
  }
  for (const jobName of ['test-installer', 'build-frontend', 'component-profile-checks']) {
    const needs = releaseWorkflow?.jobs?.[jobName]?.needs;
    if (!(needs === 'release-preflight' || (Array.isArray(needs) && needs.includes('release-preflight')))) {
      throw new Error(`release.yml: ${jobName} must wait for release-preflight`);
    }
  }
  if (!String(releaseWorkflow?.jobs?.['publish-linux']?.if ?? '').includes('preflight_only')) {
    throw new Error('release.yml: publishing must be disabled in preflight_only mode');
  }
  for (const reference of [
    '${{ vars.SUI_RELEASE_TRUST_ROOTS_B64 }}',
    '${{ vars.SUI_RELEASE_SIGNING_KEY_ID }}',
    '${{ secrets.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 }}',
  ]) {
    if (!releaseSource.includes(reference)) throw new Error(`release.yml: required signing input is missing: ${reference}`);
  }

  const dockerWorkflow = YAML.parse(fs.readFileSync(path.join(workflowDir, 'docker.yml'), 'utf8'));
  const dockerBuildSteps = dockerWorkflow?.jobs?.build?.steps ?? [];
  if (!dockerBuildSteps.some((step) => step.name === 'Preflight Docker image' && String(step.if ?? '').includes('preflight_only'))) {
    throw new Error('docker.yml: manual preflight must build images without publishing');
  }
  if (!String(dockerWorkflow?.jobs?.publish?.if ?? '').includes('preflight_only')) {
    throw new Error('docker.yml: publishing must be disabled in preflight_only mode');
  }

  console.log(`ok release contracts (Go ${goVersion}, Windows amd64/cgo)`);
}
