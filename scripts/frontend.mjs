// Product consumes a pinned distribution. Source is materialized only as a verified audit/dev fixture.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {productRoot} from './component-source-inputs.mjs'
import {prepareFrontendBuildInputs} from './frontend-build-inputs.mjs'
import {frontendFixtureDirectory} from './frontend-source.mjs'
import {readFrontendPin, resolveFrontendDistribution, verifyFrontendDistributionDirectory, selectFrontendArtifact} from './frontend-artifacts.mjs'
const tasks = new Set(['build','verify','install','source','dev','typecheck','test','test:unit','test:e2e','lint','preview','browser-install'])
export function npmCLI(env = process.env) {
  const nodeDirectory = path.dirname(process.execPath)
  const candidates = [env.npm_execpath, path.join(nodeDirectory, 'node_modules/npm/bin/npm-cli.js'), path.join(nodeDirectory, '../lib/node_modules/npm/bin/npm-cli.js')]
  const searchPath = Object.entries(env).find(([name]) => name.toLowerCase() === 'path')?.[1] || ''
  for (const directory of searchPath.split(path.delimiter).filter(Boolean)) {
    const executable = path.join(directory, 'npm')
    if (fs.existsSync(executable)) {
      const resolved = fs.realpathSync.native(executable)
      if (path.basename(resolved) === 'npm-cli.js') candidates.push(resolved)
    }
  }
  const cli = candidates.find(filename => filename && fs.existsSync(filename))
  assert.ok(cli, 'npm CLI unavailable for the selected Node toolchain')
  return fs.realpathSync.native(cli)
}

export async function runFrontendTask(task,args=[],root=productRoot,options={}) {
  assert.ok(tasks.has(task),'unsupported Product frontend task')
  root=fs.realpathSync.native(root)
  if(task==='build'||task==='verify') {
    if(task==='build')assert.deepEqual(args,[],'Product build accepts only the admitted profile environment')
    else {assert.equal(args.length,2);assert.equal(args[0],'--dist','Frontend verification requires an explicit distribution')}
    const pin=readFrontendPin(root), input=prepareFrontendBuildInputs(root)
    assert.equal(input.manifestSHA256,pin.input.manifestSHA256,'Product Frontend input tuple drift')
    assert.equal(input.sourceLockDigest,pin.input.sourceLockDigest,'Product component source authority drift')
    const options={root,pin,key:selectFrontendArtifact(pin)}
    const artifact=task==='build'?await resolveFrontendDistribution({...options,destination:path.join(root,'.artifacts/frontend-dist')}):await verifyFrontendDistributionDirectory(path.resolve(root,args[1]),options)
    console.log(JSON.stringify({schema:'solovey.frontend-consumption/v1',inputSHA256:input.manifestSHA256,...artifact}))
    return 0
  }
  const frontendRoot=options.frontendRoot || await frontendFixtureDirectory()
  if(task==='source'){assert.deepEqual(args,[]);console.log(frontendRoot);return 0}
  const pin=readFrontendPin(), env={...process.env}
  assert.equal(process.version,'v'+pin.toolchain.node,'Product frontend tooling Node drift')
  if(!['install','lint','browser-install'].includes(task)) {
    const input=prepareFrontendBuildInputs(root,{...options,frontendRoot,directory:path.join(frontendRoot,'.component-inputs')})
    if(path.resolve(root)===path.resolve(productRoot))assert.equal(input.manifestSHA256,pin.input.manifestSHA256,'Product Frontend fixture input drift')
    env.SOLOVEY_UI_INPUTS_SHA256=input.manifestSHA256
  }
  Object.assign(env,{SUI_E2E_SERVER_SCRIPT:path.join(root,'tests/e2e/run-server.js'),SUI_E2E_SERVER_CWD:root,SUI_E2E_FRONTEND_DIR:frontendRoot,SUI_E2E_IMPORT_FIXTURES_DIR:path.join(root,'test-db'),SUI_E2E_RESULT_DIR:process.env.SUI_E2E_RESULT_DIR||path.join(root,'tests/baseline/e2e')})
  if(task==='preview')await resolveFrontendDistribution({root,destination:path.join(frontendRoot,'dist')})
  const npmProgram=npmCLI(env)
  const npmVersion=spawnSync(process.execPath,[npmProgram,'--version'],{encoding:'utf8',env,windowsHide:true})
  assert.equal(npmVersion.status,0,'npm toolchain unavailable');assert.equal(npmVersion.stdout.trim(),pin.toolchain.npm,'Product frontend tooling npm drift')
  const npmArgs=task==='install'?['ci','--prefer-offline','--no-audit','--no-fund']:['run',task,...(args.length?['--',...args]:[])]
  const invocation=task==='browser-install'?[path.join(frontendRoot,'node_modules/playwright/cli.js'),'install',...args]:[npmProgram,...npmArgs]
  const result=spawnSync(process.execPath,invocation,{cwd:frontendRoot,env,stdio:'inherit',windowsHide:true})
  if(result.error)throw result.error
  return result.status??1
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  const [task,...args]=process.argv.slice(2)
  process.exitCode=await runFrontendTask(task,args)
}
