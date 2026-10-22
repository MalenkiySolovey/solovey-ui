// Run from the canonical Panel checkout. No network or implicit dependency fetch.
import fs from 'node:fs'
import path from 'node:path'
import {fileURLToPath} from 'node:url'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'

const args=process.argv.slice(2),check=args.includes('--check')
const destinations=args.filter(arg=>arg!=='--check')
if(destinations.length!==1)throw Error('Usage: node contracts/tools/pin.mjs NODE_CHECKOUT [--check]')
const source=fileURLToPath(new URL('../../',import.meta.url))
const target=path.resolve(destinations[0])
const git=(root,...command)=>execFileSync('git',['-C',root,...command],{encoding:'utf8'}).trim()
if(!/github\.com[/:]MalenkiySolovey\/solovey-node(?:\.git)?$/.test(git(target,'remote','get-url','origin')))
  throw Error('Destination must be the existing first-party Node checkout')
if(git(source,'status','--porcelain','--','contracts'))throw Error('Commit the canonical contract before pinning')
const normalize=text=>text.replaceAll('\r\n','\n')
const digest=text=>createHash('sha256').update(text).digest('hex')
const canonical=path.join(source,'contracts/management/v1')
const manifestText=normalize(fs.readFileSync(path.join(canonical,'manifest.json'),'utf8'))
const manifest=JSON.parse(manifestText)
const pin={canonicalRepository:'MalenkiySolovey/solovey-ui',panelContractCommit:git(source,'rev-parse','HEAD'),
  contractVersion:manifest.contractVersion,manifestSHA256:digest(manifestText),schemaSHA256:manifest.schemaSHA256,
  generatedSHA256:manifest.generatedSHA256,vectorsSHA256:manifest.vectorsSHA256,bundleSHA256:manifest.bundleSHA256}
function transfer(relative,text) {
  const destination=path.join(target,relative)
  if(check){if(!fs.existsSync(destination)||normalize(fs.readFileSync(destination,'utf8'))!==text)throw Error(`Cross-repo drift: ${relative}`)}
  else {fs.mkdirSync(path.dirname(destination),{recursive:true});fs.writeFileSync(destination,text)}
}
for(const name of [...Object.keys(manifest.files),'manifest.json'])
  transfer(`contracts/management/v1/${name}`,normalize(fs.readFileSync(path.join(canonical,name),'utf8')))
transfer('contracts/tools/generate.mjs',normalize(fs.readFileSync(path.join(source,'contracts/tools/generate.mjs'),'utf8')))
if(check){
  const existing=JSON.parse(fs.readFileSync(path.join(target,'contracts/PIN.json'),'utf8'))
  for(const key of Object.keys(pin).filter(key=>key!=='panelContractCommit'))
    if(existing[key]!==pin[key])throw Error(`Cross-repo provenance drift: ${key}`)
  // A pin may name the qualified PR head accepted through a later merge.
  if(!/^[a-f0-9]{40}$/.test(existing.panelContractCommit))throw Error('Invalid source provenance')
  execFileSync('git',['-C',source,'merge-base','--is-ancestor',existing.panelContractCommit,'HEAD'])
} else transfer('contracts/PIN.json',JSON.stringify(pin,null,2)+'\n')
console.log(JSON.stringify({mode:check?'compare':'pin',contractVersion:pin.contractVersion,bundleSHA256:pin.bundleSHA256}))
