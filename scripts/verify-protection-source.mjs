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
const portable = ["domain","service/decision","service/scoring","service/policy","service/response","service/enforcement","service/resources","service/events","service/observationio","service/observation","service/durable","service/operations","service/helpercontract","service/health","service/graylist","service/interception","service/localproxy","service/artifactcontract","service/firewall","service/udpguard","service/nativefallback","service/fronting"];
// These cases exercise real Product owners in the normal Product suite. Every
// portable production file and independent feature test is included below.
const productIntegrationTests = new Set(
[
  "service/enforcement/intent_test.go",
  "service/firewall/baseline_source_fixture_test.go",
  "service/firewall/composition_test.go",
  "service/firewall/contribution_identity_test.go",
  "service/firewall/durable_fixtures_test.go",
  "service/firewall/endpoint_plan_test.go",
  "service/firewall/expired_process_test.go",
  "service/firewall/network_plan_test.go",
  "service/firewall/network_resource_test.go",
  "service/firewall/operator_resolution_test.go",
  "service/firewall/package_replacement_test.go",
  "service/firewall/policy_boundary_test.go",
  "service/firewall/preview_test.go",
  "service/firewall/preview_wire_test.go",
  "service/firewall/retained_public_test.go",
  "service/firewall/runtime_crash_test.go",
  "service/firewall/runtime_sqlite_lock_test.go",
  "service/firewall/runtime_terminalization_test.go",
  "service/firewall/ssh_management_chain_linux_test.go",
  "service/firewall/ssh_management_chain_test.go",
  "service/firewall/test_doubles_test.go",
  "service/firewall/udp_flow_policy_test.go",
  "service/firewall/workflow_fake_ci_test.go",
  "service/firewall/workflow_test.go",
  "service/fronting/performance_isolated_v2_test.go",
  "service/fronting/performance_v2_test.go",
  "service/fronting/product_contract_architecture_test.go",
  "service/fronting/semantic_v2_test.go",
  "service/fronting/sni_fallback_workflow_v2_test.go",
  "service/fronting/sni_preread_v2_test.go",
  "service/fronting/sni_workflow_v2_test.go",
  "service/fronting/workflow_test.go",
  "service/fronting/workflow_v2_test.go",
  "service/interception/performance_test.go",
  "service/interception/service_test.go",
  "service/localproxy/localproxy_test.go",
  "service/nativefallback/core_checkpoint_retention_integration_test.go",
  "service/nativefallback/performance_test.go",
  "service/nativefallback/planner_test.go",
  "service/nativefallback/workflow_test.go",
  "service/observation/worker_test.go",
  "service/operations/manager_test.go",
  "service/operations/runtime_recovery_test.go"
]
);const systemModule = 'golang.org/x/sys';
const systemVersion = metadata.Require.find(item => item.Path === systemModule)?.Version;
assert.ok(systemVersion, 'journal process identity uses the accepted system module');
assert.ok(!(metadata.Replace ?? []).some(item => item.Old.Path === systemModule), 'system module must use published source');
const temp = fs.realpathSync(os.tmpdir());
const fixture = fs.mkdtempSync(path.join(temp, 'solovey-protection-source-'));
let files = 0;
for (const relative of portable) {
  const source = path.join(root, 'components/server-protection', relative);
  const destination = path.join(fixture, relative);
  fs.mkdirSync(destination, {recursive: true});
  const testdata = path.join(source, 'testdata');
  if (fs.existsSync(testdata)) fs.cpSync(testdata, path.join(destination, 'testdata'), {recursive: true});
  for (const name of fs.readdirSync(source)) {
    if (!name.endsWith('.go') || productIntegrationTests.has(relative + '/' + name)) continue;
    const content = fs.readFileSync(path.join(source, name), 'utf8').replaceAll(prefix, identity + '/');
    assert.ok(!content.includes('github.com/MalenkiySolovey/solovey-ui/'), 'private source in ' + relative + '/' + name);
    fs.writeFileSync(path.join(destination, name), content);
    files++;
  }
}
fs.writeFileSync(path.join(fixture, 'go.mod'), `module ${identity}\n\ngo ${metadata.Go}\n\nrequire (\n\t${hostModule} ${hostVersion}\n\t${systemModule} ${systemVersion}\n)\n`);
fs.copyFileSync(path.join(root, 'go.sum'), path.join(fixture, 'go.sum'));
const env = {...process.env, GOWORK: 'off', GOFLAGS: '-mod=readonly'};
const run = args => execFileSync('go', args, {cwd: fixture, env, encoding: 'utf8', windowsHide: true, maxBuffer: 4 << 20});
const dependencies = run(['list', '-deps', '-test', '-f', '{{if not .Standard}}{{.ImportPath}}{{end}}', './...']).trim().split(/\r?\n/).filter(Boolean);
for (const dependency of dependencies) {
  assert.ok([identity, hostModule, systemModule].some(module => dependency.startsWith(module + '/')), 'unexpected independent dependency: ' + dependency);
}
process.stdout.write(run(['test', '-race', '-p', '1', '-count=1', '-timeout=3m', './...']));
process.stdout.write(run(['vet', './...']));
assert.ok(!fs.existsSync(path.join(fixture, 'componenthost')), 'Product Host source must not be copied');
console.log(JSON.stringify({status: 'PASS', module: identity, hostModule, hostVersion, packages: portable, sourceFiles: files, productIntegrationTests: [...productIntegrationTests], dependencies, ProductHostSourcePackages: 0, localReplacements: 0, os: process.platform}));
// This path was created by this invocation and is checked against its temp
// parent before removing disposable qualification outputs.
assert.equal(path.dirname(fs.realpathSync(fixture)), temp);
assert.ok(path.basename(fixture).startsWith('solovey-protection-source-'));
fs.rmSync(fixture, {recursive: true});
