import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const ts = require('typescript')
const { parse } = require('@vue/compiler-sfc')

// Parse actual fallback expressions, not comments, translations or an explicit
// Direct selection. Existing omitted-final behavior is a frozen compatibility
// seam; SEC-2 must decide its migration independently of this enforcement PR.
export function inspectPolicy(source, filename) {
  const blocks = filename.endsWith('.vue')
    ? (() => {
        const { descriptor, errors } = parse(source, { filename })
        if (errors.length) throw Error(`Cannot parse ${filename}: ${errors[0]}`)
        return [descriptor.script, descriptor.scriptSetup].filter(Boolean).map(block => block.content)
      })()
    : [source]
  const found = []
  for (const block of blocks) {
    const tree = ts.createSourceFile(filename, block, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
    if (tree.parseDiagnostics.length) throw Error(`Cannot parse policy source ${filename}`)
    const direct = node => ts.isStringLiteral(node) && node.text === 'direct'
    const visit = node => {
      if (ts.isBinaryExpression(node) &&
          [ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(node.operatorToken.kind) && direct(node.right)) {
        found.push(node.getText(tree).replace(/\s+/g, ' '))
      }
      if (ts.isConditionalExpression(node) && (direct(node.whenTrue) || direct(node.whenFalse))) {
        found.push(node.getText(tree).replace(/\s+/g, ' '))
      }
      ts.forEachChild(node, visit)
    }
    visit(tree)
  }
  return found.sort()
}

export function inspectFrontendPolicy(root) {
  const sites = []
  const visit = directory => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name)
      if (entry.isDirectory()) {
        if (!['node_modules', 'dist', 'testdata'].includes(entry.name)) visit(absolute)
      } else if (/\.(?:ts|vue)$/.test(entry.name) && !/\.(?:test|spec)\.ts$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
        for (const expression of inspectPolicy(fs.readFileSync(absolute, 'utf8'), absolute)) {
          sites.push({ path: path.relative(root, absolute).replaceAll('\\', '/'), expression })
        }
      }
    }
  }
  visit(path.join(root, 'frontend/src'))
  for (const entry of fs.readdirSync(path.join(root, 'components'), { withFileTypes: true })) {
    const frontend = path.join(root, 'components', entry.name, 'frontend')
    if (entry.isDirectory() && fs.existsSync(frontend)) visit(frontend)
  }
  return sites.sort((a, b) => a.path.localeCompare(b.path) || a.expression.localeCompare(b.expression))
}

export function unreviewedFrontendPolicy(sites) {
  // Each site was inspected in its current editor/preview owner. These are
  // compatibility allowances, not approval of a new fallback at another site.
  const legacy = [
    { path: 'frontend/src/components/protocols/Failover.vue', expression: "this.$props.data.final || 'direct'" },
    { path: 'frontend/src/layouts/modals/Rule.logic.ts', expression: "this.$props.outTags[0]?? 'direct'" },
    { path: 'frontend/src/features/useInboundEditor.ts', expression: 'draft.type || "direct"' },
    { path: 'frontend/src/components/presets/RegionalPresetDrawer.vue', expression: "outboundItems.value.some(item => item.value === 'direct') ? 'direct' : (props.outboundTags[0] ?? '')" },
    { path: 'frontend/src/components/presets/routingDnsPresets.ts', expression: "direction === 'direct' ? 'proxy' : 'direct'" },
    { path: 'frontend/src/components/presets/routingDnsPresets.ts', expression: "matchingRuleSets.some(item => directionFromTag(String(item?.tag ?? ''), region) === 'proxy') ? 'proxy' : 'direct'" },
  ]
  const remaining = legacy.map(site => JSON.stringify(site))
  return sites.filter(site => {
    const index = remaining.indexOf(JSON.stringify(site))
    if (index < 0) return true
    remaining.splice(index, 1)
    return false
  })
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const added = unreviewedFrontendPolicy(inspectFrontendPolicy(root))
  if (added.length) throw Error(`Unreviewed Direct fallback policy: ${JSON.stringify(added)}`)
  console.log('Frontend Direct fallback budget passed')
}
