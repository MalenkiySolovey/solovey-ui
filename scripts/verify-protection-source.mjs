import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {execFileSync} from 'node:child_process';

const root = process.cwd();
const metadata = JSON.parse(execFileSync('go', ['mod', 'edit', '-json'], {encoding: 'utf8'}));
const hostModule = 'github.com/MalenkiySolovey/solovey-host';
const hostVersion = metadata.Require.find(item => item.Path === hostModule)?.Version;
assert.match(hostVersion ?? '', /^v\d+\.\d+\.\d+$/);
assert.ok(!(metadata.Replace ?? []).some(item => item.Old.Path === hostModule), 'Host must use published source');
const identity = 'example.invalid/protection-source-check';
const prefix = 'github.com/MalenkiySolovey/solovey-ui/components/server-protection/';
const portable = ['domain', ...['decision', 'scoring', 'policy', 'response', 'enforcement', 'resources'].map(name => 'service/' + name)];
const temp = fs.realpathSync(os.tmpdir());
const fixture = fs.mkdtempSync(path.join(temp, 'solovey-protection-source-'));
let files = 0;
for (const relative of portable) {
  const source = path.join(root, 'components/server-protection', relative);
  const destination = path.join(fixture, relative);
  fs.mkdirSync(destination, {recursive: true});
  for (const name of fs.readdirSync(source)) {
    // This test exercises the actual Product Planner and deployment adapters in
    // the normal Product suite. Feature algorithm tests run in this fixture.
    if (!name.endsWith('.go') || relative === 'service/enforcement' && name === 'intent_test.go') continue;
    const content = fs.readFileSync(path.join(source, name), 'utf8').replaceAll(prefix, identity + '/');
    assert.ok(!content.includes('github.com/MalenkiySolovey/solovey-ui/'), 'private source in ' + relative + '/' + name);
    fs.writeFileSync(path.join(destination, name), content);
    files++;
  }
}
fs.writeFileSync(path.join(fixture, 'go.mod'), `module ${identity}\n\ngo ${metadata.Go}\n\nrequire ${hostModule} ${hostVersion}\n`);
fs.copyFileSync(path.join(root, 'go.sum'), path.join(fixture, 'go.sum'));
const env = {...process.env, GOWORK: 'off', GOFLAGS: '-mod=readonly'};
const run = args => execFileSync('go', args, {cwd: fixture, env, encoding: 'utf8', windowsHide: true, maxBuffer: 4 << 20});
const dependencies = run(['list', '-deps', '-test', '-f', '{{if not .Standard}}{{.ImportPath}}{{end}}', './...']).trim().split(/\r?\n/).filter(Boolean);
for (const dependency of dependencies) {
  assert.ok(dependency.startsWith(identity + '/') || dependency.startsWith(hostModule + '/'), 'unexpected independent dependency: ' + dependency);
}
process.stdout.write(run(['test', '-race', '-count=1', '-timeout=3m', './...']));
process.stdout.write(run(['vet', './...']));
assert.ok(!fs.existsSync(path.join(fixture, 'componenthost')), 'Product Host source must not be copied');
console.log(JSON.stringify({status: 'PASS', module: identity, hostModule, hostVersion, packages: portable, sourceFiles: files, dependencies, ProductHostSourcePackages: 0, localReplacements: 0, os: process.platform}));
// This path was created by this invocation and is checked against its temp
// parent before removing disposable qualification outputs.
assert.equal(path.dirname(fs.realpathSync(fixture)), temp);
assert.ok(path.basename(fixture).startsWith('solovey-protection-source-'));
fs.rmSync(fixture, {recursive: true});
