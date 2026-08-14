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
  }
  console.log(`ok ${file}`);
}

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
