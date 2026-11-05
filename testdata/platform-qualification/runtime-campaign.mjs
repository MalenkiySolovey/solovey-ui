import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import assert from 'node:assert/strict'
import {spawn,spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
const cfg=JSON.parse(fs.readFileSync(process.argv[2],'utf8').replace(/^\uFEFF/,''))
const scratch=path.resolve(cfg.outputRoot??path.dirname(fileURLToPath(import.meta.url)))
const privateDir=path.join(scratch,cfg.name)
fs.mkdirSync(privateDir,{recursive:true,mode:0o700})
const privateFile=path.join(privateDir,'credentials.json')
const credentials=fs.existsSync(privateFile)?JSON.parse(fs.readFileSync(privateFile)): {password:crypto.randomBytes(32).toString('base64url'),key:crypto.randomBytes(32).toString('base64')}
fs.writeFileSync(privateFile,JSON.stringify(credentials),{mode:0o600})
const env={...process.env,SUI_DB_FOLDER:path.join(privateDir,'db'),SUI_CACHE_FOLDER:path.join(privateDir,'cache'),SUI_SECRETBOX_KEY:credentials.key,...cfg.env}
if(!cfg.executor)fs.mkdirSync(env.SUI_DB_FOLDER,{recursive:true,mode:0o700})
const checks=[]
let completion='IN_PROGRESS'
const writeResult=()=>fs.writeFileSync(path.join(privateDir,'result.json'),JSON.stringify({schema:'solovey.d12-runtime/v1',host:process.platform,hostArch:process.arch,mode:cfg.mode??'native-process',sourceCommit:cfg.sourceCommit??null,panelSha256:cfg.panelSha256??null,completion,checks},null,2)+'\n')
const pass=(name,details={})=>{checks.push({name,result:'PASS',...details});console.log(`PASS ${cfg.name} ${name}`);writeResult()}
const panelCommand=exe=>cfg.executor??[...(cfg.commandPrefix??[]),exe]
const run=(exe,args,input)=>{const command=exe===cfg.peer?(cfg.peerExecutor??[exe]):panelCommand(exe);const r=spawnSync(command[0],[...command.slice(1),...args],{env,cwd:privateDir,input,encoding:'utf8',timeout:60000,maxBuffer:8*1024*1024,windowsHide:true});fs.writeFileSync(path.join(privateDir,'last-cli.log'),(r.stdout??'')+(r.stderr??''));assert.equal(r.status,0,`CLI ${args[0]} failed: ${r.error??r.stderr}`);return r.stdout}
let child,log,cookie='',token=''
const origin=`http://127.0.0.1:${cfg.port}`,api=origin+'/d12/api'
async function call(route,data,raw=false){
 const headers={Origin:origin,Cookie:cookie,'X-CSRF-Token':token}
 if(data){headers['Content-Type']='application/x-www-form-urlencoded';data=new URLSearchParams(data)}
 const r=await fetch(api+route,{method:data?'POST':'GET',headers,body:data,signal:AbortSignal.timeout(30000)})
 const cookies=r.headers.getSetCookie();if(cookies.length)cookie=cookies.map(c=>c.split(';')[0]).join('; ')
 if(raw)return Buffer.from(await r.arrayBuffer())
 const v=await r.json();fs.writeFileSync(path.join(privateDir,'last-api.json'),JSON.stringify(v));assert.equal(v.success,true,`API ${route} HTTP ${r.status}: ${v.msg??v.reason??''}`);return v.obj
}
async function bodyCall(route,body,extra={}) {
 const headers={Origin:origin,Cookie:cookie,'X-CSRF-Token':token,...extra}
 if(!(body instanceof FormData)){headers['Content-Type']='application/json';body=JSON.stringify(body)}
 const r=await fetch(api+route,{method:'POST',headers,body,signal:AbortSignal.timeout(120000)})
 const cookies=r.headers.getSetCookie();if(cookies.length)cookie=cookies.map(c=>c.split(';')[0]).join('; ')
 const v=await r.json();fs.writeFileSync(path.join(privateDir,'last-body-api.json'),JSON.stringify(v));assert.equal(v.success,true,`API ${route} HTTP ${r.status}: ${v.msg??''}`);return v.obj
}
async function login(){cookie='';token='';token=(await call('/csrf')).token;await call('/login',{user:'d12fixture',pass:credentials.password});token=(await call('/csrf')).token}
async function start(exe,label){
 cookie='';token='';log=fs.openSync(path.join(privateDir,label+'.log'),'a')
 const command=cfg.startCommand??panelCommand(exe)
 child=spawn(command[0],command.slice(1),{env,cwd:privateDir,stdio:['ignore',log,log],windowsHide:true})
 let ready=false;for(let i=0;i<90;i++){if(child.exitCode!==null)throw Error(`${label} exited ${child.exitCode}`);try{await call('/csrf');ready=true;break}catch{}await new Promise(r=>setTimeout(r,500))}
 assert(ready,`${label} not ready`);token=(await call('/csrf')).token
 await call('/login',{user:'d12fixture',pass:credentials.password});token=(await call('/csrf')).token
 const status=await call('/runtime/status');fs.writeFileSync(path.join(privateDir,label+'-runtime.json'),JSON.stringify(status,null,2))
 pass(label+' startup and authenticated API')
}
async function stop(){if(!child)return;const p=child;child=undefined;const closed=new Promise(r=>p.once('close',r));if(cfg.stopCommand){const r=spawnSync(cfg.stopCommand[0],cfg.stopCommand.slice(1),{encoding:'utf8',timeout:20000});assert.equal(r.status,0,r.stderr)}else p.kill('SIGTERM');if(p.exitCode===null)await Promise.race([closed,new Promise((_,reject)=>setTimeout(()=>reject(Error('process did not stop')),10000))]);fs.closeSync(log)}
const waitMutation=async()=>{
 for(let attempt=0;attempt<12;attempt++){
  const pressure=await call('/v1/operations/pressure');fs.writeFileSync(path.join(privateDir,'resource-pressure.json'),JSON.stringify(pressure,null,2))
  if(pressure.admissionEffects?.heavy_mutation?.allowed) return
  await new Promise(r=>setTimeout(r,30000))
 }
 throw Error('resource admission did not permit the requested mutation')
}
const save=async(object,action,data,initUsers='')=>{await waitMutation();return call('/save',{object,action,data:JSON.stringify(data),initUsers})}
async function rawConfig(){return JSON.parse((await call('/singbox-config',null,true)).toString())}
async function users(){const v=await call('/clients');const rows=Array.isArray(v)?v:(v.clients??[]);return Promise.all(rows.map(async u=>u.config?u:((await call('/clients?id='+u.id)).clients??[])[0]))}
async function inbounds(){const v=await call('/inbounds');return Array.isArray(v)?v:(v.inbounds??[])}
async function traffic(label){
 const c=await rawConfig();const native=c.inbounds.filter(i=>i.tag?.startsWith('d12-mieru-'))
 assert.equal(native.length,2)
 for(const inbound of native){assert.equal(inbound.users.length,1);const u=inbound.users[0];const input=JSON.stringify({Host:'127.0.0.1',Port:inbound.listen_port,Transport:inbound.transport,User:u.username??u.name,Password:u.password})
 const result=JSON.parse(run(cfg.peer,[],input));assert(result.authenticated&&result.tcp&&result.udp)}
 pass(label+' authenticated Mieru TCP and UDP over both carriers')
}
try{
 assert.equal(JSON.parse(run(cfg.extended,['core-info'])).id,'shtorm-extended')
 run(cfg.extended,['migrate']);run(cfg.extended,['setting','--port',String(cfg.port),'--path','/d12/','--subPort',String(cfg.port+1)]);run(cfg.extended,['admin','--username','d12fixture','--password',credentials.password])
 await start(cfg.extended,'extended-full')
 const caps=await call('/capabilities');fs.writeFileSync(path.join(privateDir,'capabilities.json'),JSON.stringify(caps,null,2));pass('capabilities observed')
 let list=await users();assert(Array.isArray(list))
 if(!list.some(u=>u.name==='d12-native-user'))await save('clients','new',{name:'d12-native-user',enable:true,config:{},inbounds:[],links:[],volume:0,expiry:0})
 list=await users();let user=list.find(u=>u.name==='d12-native-user');assert(user)
 for(const [index,transport] of ['TCP','UDP'].entries()){
  if(!(await inbounds()).some(i=>i.tag===`d12-mieru-${transport}`))await save('inbounds','new',{type:'mieru',tag:`d12-mieru-${transport}`,listen:cfg.listenHost??'127.0.0.1',listen_port:cfg.port+10+index,transport,user_hint_is_mandatory:true,mtu:1400},String(user.id))
 }
 await traffic('initial')
 user=(await users()).find(u=>u.name==='d12-native-user');const secretRef=JSON.stringify(user.config.mieru.password)
 assert(user.config.mieru.password.reference);pass('native credential uses SecretRef')
 await call('/restartSb',{});await traffic('core restart')
 const naive={outbounds:[{type:'naive',tag:'d12-naive',server:'127.0.0.1',server_port:443,username:'fixture',password:'private-qualification',tls:{enabled:true,server_name:'fixture.invalid'}}]}
 const validation=JSON.parse(run(cfg.extended,['core-check'],JSON.stringify(naive)));fs.writeFileSync(path.join(privateDir,'naive-validation.json'),JSON.stringify(validation,null,2));assert(validation.valid??validation.success??validation.state==='VALID');pass('native Naive constructor and config validation')
 const outboundView=await call('/outbounds');const outboundRows=Array.isArray(outboundView)?outboundView:(outboundView.outbounds??[]);let nativeOutbound=outboundRows.find(o=>o.tag==='d12-naive')
 if(!nativeOutbound)await save('outbounds','new',naive.outbounds[0])
 const nativeStatus=await call('/runtime/status');assert.equal(nativeStatus.status.state,'running');assert((await rawConfig()).outbounds.some(o=>o.tag==='d12-naive'&&o.type==='naive'));pass('Naive native engine startup')
 if(process.platform==='win32'){
  const script='$ErrorActionPreference="Stop"; $m=(Get-Process -Id '+child.pid+').Modules | Where-Object ModuleName -EQ "libcronet.dll"; if (!$m) { throw "Native companion is not loaded" }; $stream=[IO.File]::OpenRead($m.FileName); try { $sha=[Security.Cryptography.SHA256]::Create(); $digest=([BitConverter]::ToString($sha.ComputeHash($stream))).Replace("-","").ToLowerInvariant() } finally { $stream.Dispose() }; @{path=$m.FileName;sha256=$digest} | ConvertTo-Json -Compress'
  const probe=spawnSync('powershell.exe',['-NoLogo','-NoProfile','-NonInteractive','-Command',script],{encoding:'utf8',windowsHide:true});assert.equal(probe.status,0,probe.stderr)
  const library=JSON.parse(probe.stdout);assert.equal(path.resolve(library.path).toLowerCase(),path.join(path.dirname(cfg.extended),'libcronet.dll').toLowerCase());if(cfg.expectedNativeSha)assert.equal(library.sha256,cfg.expectedNativeSha);pass('installed Cronet DLL loaded by the actual panel',{nativeSha256:library.sha256})
 }
 const backupFile=cfg.backupHostPath??path.join(privateDir,'backup.db')
 run(cfg.extended,['backup','--output',cfg.backupPath??backupFile]);assert(fs.statSync(backupFile).size>0);pass('canonical database backup export')
 const status=await call('/status');fs.writeFileSync(path.join(privateDir,'health-status.json'),JSON.stringify(status,null,2));pass('health snapshot observed')
 await stop();await start(cfg.extended,'extended-full-restart');await traffic('process restart');assert.equal(JSON.stringify((await users()).find(u=>u.id===user.id).config.mieru.password),secretRef);pass('credential identity retained across restart')
 if(cfg.official){await stop();assert.equal(JSON.parse(run(cfg.official,['core-info'])).id,'sing-box');await start(cfg.official,'official');const desired=await inbounds();assert.equal(desired.filter(i=>i.tag?.startsWith('d12-mieru-')).length,2);const effective=await rawConfig();assert(!(effective.inbounds??[]).some(i=>i.type==='mieru'));assert.equal(JSON.stringify((await users()).find(u=>u.id===user.id).config.mieru.password),secretRef);pass('Official retains unsupported desired state and SecretRef while excluding effective runtime');await stop();await start(cfg.extended,'extended-return');await traffic('provider return');pass('return to Extended revalidates preserved state')}
 if(cfg.core){await stop();assert.equal(JSON.parse(run(cfg.core,['core-info'])).id,'shtorm-extended');await start(cfg.core,'extended-core');await traffic('Core profile');assert.equal(JSON.stringify((await users()).find(u=>u.id===user.id).config.mieru.password),secretRef);pass('Core profile persistence and credential identity')}
 if(!cfg.executor&&!cfg.commandPrefix){
  await stop();const unavailableRoot=path.join(privateDir,'failed-start-not-a-directory');fs.writeFileSync(unavailableRoot,'deterministic unavailable state directory')
  const fd=fs.openSync(path.join(privateDir,'failed-start.log'),'w');const failed=spawn(cfg.extended,[],{env:{...env,SUI_DB_FOLDER:unavailableRoot},cwd:privateDir,stdio:['ignore',fd,fd],windowsHide:true});const code=await Promise.race([new Promise(r=>failed.once('close',r)),new Promise(r=>setTimeout(()=>{failed.kill();r(null)},15000))]);fs.closeSync(fd);assert(code!==null&&code!==0,'unavailable configured state directory did not reject startup');pass('failed start rejects unavailable state directory')
  await start(cfg.extended,'failed-start-recovery');await traffic('failed start recovery')
 }
 await waitMutation()
 const upload=()=>{const body=new FormData();body.append('db',new Blob([fs.readFileSync(backupFile)]),'backup.db');return body}
 const rehearsal=await bodyCall('/v1/operations/data/restore/rehearsal',upload());assert(rehearsal.possible);pass('Backup Restore rehearsal', {revision:rehearsal.revision})
 const grant=await bodyCall('/v1/security/step-up',{method:'password',credential:credentials.password,operationKind:'backup.restore',targetDigest:crypto.createHash('sha256').update('database:restore:'+rehearsal.revision).digest('hex')});token=(await call('/csrf')).token
 const restore=upload();for(const [key,value] of Object.entries({expectedRehearsalRevision:rehearsal.revision,idempotencyKey:'d12-restore-'+crypto.randomUUID(),confirmation:'RESTORE_DATABASE_'+rehearsal.revision.slice(0,12).toUpperCase(),acknowledged:'true'}))restore.append(key,value)
 const restored=await bodyCall('/v1/operations/data/restore',restore,{'X-Step-Up-Token':grant.token});fs.writeFileSync(path.join(privateDir,'restore-result.json'),JSON.stringify(restored,null,2));assert.equal(restored.operation.state,'APPLIED');pass('authorized Backup Restore execution')
 await stop();await start(cfg.extended,'restore-restart');await traffic('restored state');assert.equal(JSON.stringify((await users()).find(u=>u.id===user.id).config.mieru.password),secretRef);pass('restore preserves SecretRef and authenticated runtime')
 completion='COMPLETE'
}catch(error){completion='FAILED';throw error}finally{await stop();writeResult()}
