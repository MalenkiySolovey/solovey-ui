import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {regularSourceTree} from './component-source-inputs.mjs'

// Go rejects overlays beneath GOMODCACHE. Materialize the verified immutable
// archive as disposable test input; the ordinary Product keeps its real pin.
export function materializeOwnerSource(source, target, expectedTree) {
  assert.ok(path.isAbsolute(source) && path.isAbsolute(target))
  assert.match(expectedTree, /^[a-f0-9]{40}$/)
  assert.ok(fs.lstatSync(source).isDirectory() && !fs.lstatSync(source).isSymbolicLink())
  // Resolve both existing authorities: Windows short names and parent junctions
  // can otherwise make a child appear to be outside its accepted source.
  source = fs.realpathSync.native(source)
  target = path.join(fs.realpathSync.native(path.dirname(target)), path.basename(target))
  const relative = path.relative(source, target)
  assert.ok(relative === '..' || relative.startsWith('..' + path.sep) || path.isAbsolute(relative), 'test destination must be outside the accepted source')
  assert.ok(!fs.existsSync(target), 'preserve existing test input')
  assert.equal(regularSourceTree(source), expectedTree, 'accepted archive source drift')
  fs.mkdirSync(target)
  let files = 0
  const copy = (from, to) => {
    for (const entry of fs.readdirSync(from, {withFileTypes: true})) {
      assert.ok(!entry.isSymbolicLink(), 'source symlink rejected')
      const input = path.join(from, entry.name), output = path.join(to, entry.name)
      if (entry.isDirectory()) { fs.mkdirSync(output); copy(input, output) }
      else { assert.ok(entry.isFile()); fs.copyFileSync(input, output, fs.constants.COPYFILE_EXCL); files++ }
    }
  }
  copy(source, target)
  assert.equal(regularSourceTree(target), expectedTree, 'generated test source differs from accepted archive')
  return {directory: target, files, tree: expectedTree}
}
