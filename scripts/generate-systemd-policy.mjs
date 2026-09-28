import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Only the address-family directive is generated. Other unit policy stays with
// its existing template owner. Legacy-root has no address-family sandbox.
const root = new URL('../', import.meta.url);
const policy = readFileSync(new URL('internal/ops/deploymentbroker/systemd-address-families.txt', root), 'utf8').trim();
if (!/^AF_[A-Z0-9]+(?: AF_[A-Z0-9]+)*$/.test(policy) || new Set(policy.split(' ')).size !== policy.split(' ').length) {
  throw new Error('Invalid native main-runtime address-family policy');
}
const check = process.argv.includes('--check');
for (const name of ['solovey-ui.service', 'deploy/systemd/solovey-ui-native-hardened.service', 'deploy/systemd/solovey-ui-native-network-advanced.service']) {
  const path = fileURLToPath(new URL(name, root));
  const source = readFileSync(path, 'utf8');
  const directives = source.match(/^RestrictAddressFamilies=.*$/gm);
  if (directives?.length !== 1) throw new Error(`${name}: expected exactly one address-family directive`);
  const output = source.replace(/^RestrictAddressFamilies=[^\r\n]*/m, `RestrictAddressFamilies=${policy}`);
  if (check && source !== output) throw new Error(`${name}: run node scripts/generate-systemd-policy.mjs`);
  if (!check && source !== output) writeFileSync(path, output);
}
