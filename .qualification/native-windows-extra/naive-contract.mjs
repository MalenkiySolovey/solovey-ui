// Adapted from executed R76S normal-owner Naive fixtures. This module is called
// inside the existing disposable packaged-native-runtime lifecycle. No private
// DTO, credential, certificate key or HTTP trace is saved.
import fs from 'node:fs'
import path from 'node:path'
import net from 'node:net'
import crypto from 'node:crypto'
import {spawn,spawnSync} from 'node:child_process'
import {createInterface} from 'node:readline'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const q=path.dirname(fileURLToPath(import.meta.url)),hash=v=>crypto.createHash('sha256').update(JSON.stringify(v)).digest('hex')
const fileHash=f=>crypto.createHash('sha256').update(fs.readFileSync(f)).digest('hex')
export async function exercisePackagedBrowser({frontend,endpoint,username:adminUser,password:adminPassword,binary:candidateBinary}){
 const identity=JSON.parse(fs.readFileSync(path.join(q,'fixture-candidate-expectation.json'),'utf8')),commit=identity.commit
 if(process.platform!=='win32'||!['x64','arm64'].includes(process.arch)||commit!=='de501586b354e8f93adc5a66984c1c61b48dbf96')throw Error('EXACT_NATIVE_WINDOWS_FIXTURE_REQUIRED')
 const binary=candidateBinary,platformArch=process.arch==='x64'?'amd64':'arm64'
 const report={test:'PACKAGED_NATIVE_WINDOWS_NAIVE_H2_H3_TLS_AND_PARENT_LIFECYCLE',sourceCommit:commit,candidateBinarySha256:fileHash(binary),startedAt:new Date().toISOString(),status:'RUNNING',evidenceClass:'HOST_NATIVE_PROVEN',environment:{platform:process.platform,architecture:process.arch,execution:'actual packaged Windows CLI and its native Cronet DLL'},assertions:[],carriers:[],cases:[],privateValuesCaptured:false,scope:'Native H2/H3 routed nonce/custom trust/auth denials, atomic unsupported TLS save, independent public-interface H3 remote close on remove/replacement/Box maintenance shutdown. No browser/external client/other platform/selective Naive parent claim.'}
 const check=(name,ok)=>{if(!ok)throw Error(name);report.assertions.push(name)}
 const require=createRequire(path.join(path.resolve(frontend),'package.json')),{request}=require('@playwright/test')
 const ctx=await request.newContext({baseURL:endpoint,timeout:45000})
 const get=async r=>{const p=await ctx.get(r),j=await p.json();if(!p.ok()||!j.success)throw Error('NATIVE_NAIVE_AUTHENTICATED_GET_FAILED');return j.obj}
 const csrf=async()=>{const p=await ctx.get('api/csrf'),j=await p.json();if(!p.ok()||!j.success)throw Error('NATIVE_NAIVE_CSRF_FAILED');return j.obj.token}
 const post=async(r,data)=>{const p=await ctx.post(r,{...data,headers:{Origin:new URL(endpoint).origin,'X-CSRF-Token':await csrf()}});return{status:p.status(),...await p.json()}}
 const save=async(object,action,data,initUsers='')=>{const r=await post('api/save',{form:{object,action,data:JSON.stringify(data),initUsers}});if(!r.success)throw Error('NATIVE_NAIVE_SAVE_'+object.toUpperCase()+'_FAILED');return r}
 let held=false,baseline,original,initialGeneration,clientID,tlsID,inboundID,outboundID,configChanged=false,successes=0,echo,wire
 const marker='w5-native-naive-'+crypto.randomBytes(5).toString('hex'),clientName=marker+'-client',tlsName=marker+'-tls',tags={inbound:marker+'-server',ingress:marker+'-ingress',outbound:marker+'-outbound'}
 const username=clientName,password=crypto.randomBytes(24).toString('hex'),socksPassword=crypto.randomBytes(24).toString('hex'),peers=[]
 const inventory=async()=>({clients:(await get('api/clients')).clients.map(x=>x.id).sort((a,b)=>a-b),inbounds:(await get('api/inbounds')).inbounds.map(x=>x.tag).sort(),outbounds:hash((await get('api/outbounds')).outbounds),services:hash((await get('api/services')).services),tls:hash((await get('api/tls')).tls),config:hash((await get('api/config')).config)})
 const maintenance=async enabled=>{const s=await get('api/runtime/status'),r=await post('api/runtime/maintenance',{data:{generation:s.status.generation,enabled}});if(!r.success)throw Error('NATIVE_NAIVE_MAINTENANCE_FAILED');held=enabled}
 const wait=async(fn,code,ms=8000)=>{const end=Date.now()+ms;while(!fn()){if(Date.now()>end)throw Error(code);await new Promise(r=>setTimeout(r,20))}}
 const start=async(kind,args,input)=>{
  const executable=path.join(q,`naive-portable-${kind}-windows-${platformArch}.exe`),child=spawn(executable,args,{windowsHide:true,stdio:['pipe','pipe','pipe']})
  child.stderr.resume();child.stdin.on('error',()=>{});const peer={child,frames:[],kind,sha256:fileHash(executable)};peers.push(peer)
  createInterface({input:child.stdout}).on('line',line=>{try{const j=JSON.parse(line);if(['READY','HELLO','ANSWERED','AUTHENTICATED','ARMED','PARENT_RESULT','STOPPED','FAIL'].includes(j.event))peer.frames.push(j)}catch{}})
  if(input)child.stdin.write(JSON.stringify(input)+'\n')
  await wait(()=>peer.frames.some(f=>f.event==='READY')||child.exitCode!==null,'NATIVE_NAIVE_PEER_READY_DEADLINE')
  check('native_'+kind+'_peer_fixed_loopback_ready',peer.frames.some(f=>f.event==='READY'&&f.loopback))
  return peer
 }
 const tcpUnused=async port=>{const s=net.createServer();try{await new Promise((resolve,reject)=>{s.once('error',reject);s.listen(port,'127.0.0.1',resolve)});return true}finally{if(s.listening)await new Promise(r=>s.close(r))}}
 const probe=async()=>{
  const s=net.createConnection({host:'127.0.0.1',port:24861});let b=Buffer.alloc(0),end=false,error=false,wake=null,stage='INGRESS';s.on('data',d=>{b=Buffer.concat([b,d]);wake?.()});s.on('end',()=>{end=true;wake?.()});s.on('close',()=>{end=true;wake?.()});s.on('error',()=>{error=true;end=true;wake?.()})
  const deadline=Date.now()+10000,read=n=>new Promise((resolve,reject)=>{const poll=()=>{if(b.length>=n){const r=b.subarray(0,n);b=b.subarray(n);clearTimeout(t);wake=null;resolve(r)}else if(end){clearTimeout(t);wake=null;reject(Error(error?'REMOTE_ERROR':'REMOTE_EOF'))}};const t=setTimeout(()=>{wake=null;reject(Error('PROBE_DEADLINE'))},Math.max(1,deadline-Date.now()));wake=poll;poll()})
  try{s.write(Buffer.from([5,1,2]));const h=await read(2);if(h[0]!==5||h[1]!==2)throw Error('SOCKS_AUTH_METHOD');const u=Buffer.from(username),p=Buffer.from(socksPassword);s.write(Buffer.concat([Buffer.from([1,u.length]),u,Buffer.from([p.length]),p]));const a=await read(2);if(a[0]!==1||a[1]!==0)throw Error('SOCKS_AUTH');stage='DESTINATION_REPLY';s.write(Buffer.from([5,1,0,1,127,0,0,1,24843>>8,24843&255]));const r=await read(4);if(r[0]!==5)throw Error('SOCKS_REPLY');if(r[1]!==0)return{nonceMatched:false,remoteDenial:true};if(r[3]===1)await read(6);else if(r[3]===4)await read(18);else if(r[3]===3){const n=await read(1);await read(n[0]+2)}else throw Error('SOCKS_ADDRESS');stage='NONCE';const n=crypto.randomBytes(32);s.write(n);return{nonceMatched:(await read(32)).equals(n),remoteDenial:false}}
  catch(e){return{nonceMatched:false,remoteDenial:['REMOTE_ERROR','REMOTE_EOF'].includes(e.message)&&stage!=='INGRESS',code:/^[A-Z_]+$/.test(e.message)?e.message:'CONTROLLED_PROBE_FAILED'}}finally{s.destroy()}
 }
 let material
 const profile=carrier=>({name:tlsName,server:{enabled:true,server_name:material.Name,certificate:[material.ServerCertificate],key:[material.ServerKey],alpn:[carrier==='h2'?'h2':'h3'],min_version:carrier==='h2'?'1.2':'1.3',max_version:carrier==='h2'?'1.2':'1.3'},client:{enabled:true,server_name:material.Name,certificate:[material.CA]},...(tlsID?{id:tlsID}:{})})
 const inbound=(carrier='h3',congestion='cubic')=>({type:'naive',tag:tags.inbound,listen:'127.0.0.1',listen_port:24862,tls_id:tlsID,network:carrier==='h2'?'tcp':'udp',quic_congestion_control:congestion,addrs:[],...(inboundID?{id:inboundID}:{})})
 const outbound=carrier=>({type:'naive',tag:tags.outbound,server:'127.0.0.1',server_port:carrier==='h2'?24860:24862,username,password,connect_timeout:'3s',quic:carrier==='h3',quic_congestion_control:carrier==='h2'?'bbr':'cubic',tls:{enabled:true,server_name:material.Name,certificate:[material.CA]},...(outboundID?{id:outboundID}:{})})
 try{
  const login=await post('api/login',{form:{user:adminUser,pass:adminPassword}});check('normal_admin_owner_login',login.success&&login.obj?.state==='authenticated')
  baseline=await inventory();original=(await get('api/config')).config;initialGeneration=(await get('api/runtime/status')).status.generation
  const direct=(await get('api/outbounds')).outbounds.find(x=>x.type==='direct');check('existing_direct_owner',!!direct)
  for(const p of [24843,24860,24861,24862])check('fixed_'+p+'_TCP_port_unused_before_fixture',await tcpUnused(p))
  const generated=spawnSync(path.join(q,`naive-portable-echo-windows-${platformArch}.exe`),['generate-private','--controller-memory-only'],{encoding:'utf8',windowsHide:true,timeout:30000,maxBuffer:65536})
  if(generated.status!==0)throw Error('NATIVE_PRIVATE_MATERIAL_FAILED');material=JSON.parse(generated.stdout)
  check('fresh_correct_host_and_private_key_fixture',new crypto.X509Certificate(material.ServerCertificate).checkHost(material.Name)===material.Name&&new crypto.X509Certificate(material.ServerCertificate).checkPrivateKey(crypto.createPrivateKey(material.ServerKey)))
  echo=await start('echo',['echo'],{Echo:'127.0.0.1:24843',StdinLifecycle:true});wire=await start('wire',['observe','--stdio-lifecycle'])
  await maintenance(true);await save('clients','new',{name:clientName,enable:true,inbounds:[],config:{naive:{username,password},socks:{username,password:socksPassword}},ipLimitMode:'monitor'});clientID=(await get('api/clients')).clients.find(x=>x.name===clientName)?.id
  await save('tls','new',profile('h2'));tlsID=(await get('api/tls')).tls.find(x=>x.name===tlsName)?.id
  await save('inbounds','new',inbound('h2'),String(clientID));inboundID=(await get('api/inbounds')).inbounds.find(x=>x.tag===tags.inbound)?.id
  await save('inbounds','new',{type:'socks',tag:tags.ingress,listen:'127.0.0.1',listen_port:24861,tls_id:0,addrs:[]},String(clientID))
  check('normal_owner_authentication_links_both_inbounds',(await get('api/clients')).clients.find(x=>x.id===clientID)?.inbounds?.length===2)
  await save('outbounds','new',outbound('h2'));outboundID=(await get('api/outbounds')).outbounds.find(x=>x.tag===tags.outbound)?.id
  const cfg=structuredClone(original);cfg.route.rules=[{inbound:[tags.ingress],action:'route',outbound:tags.outbound},{inbound:[tags.inbound],action:'route',outbound:direct.tag},...(cfg.route.rules??[])];configChanged=true;await save('config','edit',cfg);await maintenance(false)
  for(const carrier of ['h2','h3']){
   if(carrier==='h3'){await maintenance(true);await save('tls','edit',profile(carrier));await save('inbounds','edit',inbound(carrier));await save('outbounds','edit',outbound(carrier));await maintenance(false)}
   if(carrier==='h3')check('actual_UDP_only_server_has_no_TCP_fallback',await tcpUnused(24862))
   const positive=await probe();check(carrier+'_actual_native_custom_trust_authenticated_nonce',positive.nonceMatched);successes++;await wait(()=>echo.frames.filter(x=>x.event==='ANSWERED').length===successes,'NATIVE_NONCE_ACK')
   if(carrier==='h2'){const h=wire.frames.find(x=>x.event==='HELLO'&&x.direction==='server'&&x.alpn?.[0]==='h2'&&x.tlsVersion==='1.2');check('wire_unchanged_TLS12_ServerHello_selected_h2',!!h&&wire.frames.some(x=>x.event==='HELLO'&&x.direction==='client'&&x.connection===h.connection&&x.alpn.includes('h2')))}
   const proof={carrier,positive,denials:[]}
   for(const kind of ['unrelated-trust-anchor','wrong-server-name','wrong-password']){const d=outbound(carrier);if(kind==='unrelated-trust-anchor')d.tls.certificate=[material.WrongCertificate];if(kind==='wrong-server-name')d.tls.server_name='other.fixture.invalid';if(kind==='wrong-password')d.password=crypto.randomBytes(24).toString('hex');await save('outbounds','edit',d);const denied=await probe();check(carrier+'_'+kind+'_denied_before_nonce',!denied.nonceMatched&&denied.remoteDenial&&echo.frames.filter(x=>x.event==='ANSWERED').length===successes);proof.denials.push({kind,...denied})}
   await save('outbounds','edit',outbound(carrier));check(carrier+'_restored_principal_and_trust_reconnect',(await probe()).nonceMatched);successes++;await wait(()=>echo.frames.filter(x=>x.event==='ANSWERED').length===successes,'NATIVE_RECONNECT_ACK');report.carriers.push(proof)
  }
  for(const [kind,alter]of[['insecure',d=>d.tls.insecure=true],['ALPN',d=>d.tls.alpn=['h3']],['min-version',d=>d.tls.min_version='1.3'],['SPKI-pin',d=>d.tls.certificate_public_key_sha256=['AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=']],['missing-TLS',d=>delete d.tls],['disabled-TLS',d=>d.tls.enabled=false]]){const d=outbound('h3');delete d.id;d.tag=marker+'-invalid-'+kind;alter(d);const r=await post('api/save',{form:{object:'outbounds',action:'new',data:JSON.stringify(d)}});check(kind+'_save_rejected_before_durable_insert',!r.success&&!(await get('api/outbounds')).outbounds.some(x=>x.tag===d.tag))}
  const before=hash((await get('api/outbounds')).outbounds.find(x=>x.id===outboundID)),edit=outbound('h3');edit.tls.insecure=true;const response=await post('api/save',{form:{object:'outbounds',action:'edit',data:JSON.stringify(edit)}});check('invalid_edit_preserves_existing_durable_outbound',!response.success&&before===hash((await get('api/outbounds')).outbounds.find(x=>x.id===outboundID)))
  for(const kind of ['hot-removal','hot-replacement','Box-shutdown']){
   if(kind==='hot-replacement'){await maintenance(true);inboundID=undefined;await save('inbounds','new',inbound(),String(clientID));inboundID=(await get('api/inbounds')).inbounds.find(x=>x.tag===tags.inbound)?.id;await maintenance(false)}
   const peer=await start('parent',['observe'],{CA:material.CA,Name:material.Name,Username:username,Password:password,Target:'127.0.0.1:24862'});await wait(()=>peer.frames.some(x=>['AUTHENTICATED','FAIL'].includes(x.event)),'NATIVE_PARENT_AUTH_DEADLINE')
   check(kind+'_independent_authenticated_H3_parent',peer.frames.some(x=>x.event==='AUTHENTICATED'&&x.nonceMatched&&x.alpn==='h3'));successes++;await wait(()=>echo.frames.filter(x=>x.event==='ANSWERED').length===successes,'NATIVE_PARENT_NONCE_ACK')
   peer.child.stdin.end(JSON.stringify({Observe:true})+'\n');await wait(()=>peer.frames.some(x=>x.event==='ARMED'),'NATIVE_PARENT_ARM_BARRIER')
   const at=Date.now();if(kind==='hot-removal')await save('inbounds','del',tags.inbound);else if(kind==='hot-replacement')await save('inbounds','edit',inbound('h3','reno'));else await maintenance(true)
   await wait(()=>peer.frames.some(x=>x.event==='PARENT_RESULT'),'NATIVE_PARENT_OBSERVATION',4000);const result=peer.frames.find(x=>x.event==='PARENT_RESULT');check(kind+'_remote_application_close_within_two_seconds',result.remoteConnectionClose&&result.elapsedMilliseconds<=2000);report.cases.push({kind,actionMilliseconds:Date.now()-at,...result});await wait(()=>peer.frames.some(x=>x.event==='STOPPED')&&peer.child.exitCode!==null,'NATIVE_PARENT_JOIN')
  }
  report.status='PASS'
 }catch(e){report.status='FAIL';report.failure=/^[A-Za-z0-9_.:-]{1,160}$/.test(e.message)?e.message:'NATIVE_NAIVE_FIXTURE_FAILED'}
 finally{
  try{if(!held)await maintenance(true);if(configChanged)await save('config','edit',original);for(const x of (await get('api/inbounds')).inbounds.filter(x=>Object.values(tags).includes(x.tag)))await save('inbounds','del',x.tag);for(const x of (await get('api/clients')).clients.filter(x=>x.name===clientName))await save('clients','del',x.id);for(const x of (await get('api/outbounds')).outbounds.filter(x=>x.tag.startsWith(marker+'-')))await save('outbounds','del',x.tag);for(const x of (await get('api/tls')).tls.filter(x=>x.name===tlsName))await save('tls','del',x.id);await maintenance(false);if(baseline)check('normal_owner_original_inventory_and_config_restored',hash(await inventory())===hash(baseline))}catch{report.status='FAIL';report.cleanupFailure='NATIVE_OWNER_RECOVERY_REQUIRED'}
  for(const peer of peers){try{if(peer.child.exitCode===null)peer.child.stdin.end();await wait(()=>peer.child.exitCode!==null,'NATIVE_PEER_JOIN_DEADLINE',8000);check(peer.kind+'_peer_stopped_and_joined',peer.child.exitCode===0&&peer.frames.some(x=>x.event==='STOPPED'))}catch{peer.child.kill();report.status='FAIL';report.cleanupFailure='NATIVE_PEER_CLEANUP_FAILED'}}
  try{check('nonce_counter_excludes_all_denied_requests',echo?.frames.find(x=>x.event==='STOPPED')?.count===successes);for(const p of [24843,24860,24861,24862])check('no_scoped_TCP_listener_'+p,await tcpUnused(p))}catch{report.status='FAIL';report.cleanupFailure='NATIVE_LISTENER_OR_COUNTER_FAILURE'}
  await ctx.dispose();report.peers=peers.map(p=>({kind:p.kind,sha256:p.sha256,joined:p.child.exitCode===0}));report.successes=successes;report.finishedAt=new Date().toISOString();fs.writeFileSync(path.join(q,'WINDOWS_NAIVE_NATIVE_CARRIERS_AND_PARENT.json'),JSON.stringify(report,null,2)+'\n')
 }
 if(report.status!=='PASS')throw Error(report.failure??report.cleanupFailure??'NATIVE_NAIVE_QUALIFICATION_FAILED')
 return{status:'PASS',assertions:report.assertions,naiveReport:'WINDOWS_NAIVE_NATIVE_CARRIERS_AND_PARENT.json'}
}
