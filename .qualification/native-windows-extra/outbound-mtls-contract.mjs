// Product Go TLS/Trojan outbound against an independent, fixed-destination
// standard TLS wire peer. Only synthetic credentials exist in controller RAM.
import fs from 'node:fs'
import path from 'node:path'
import net from 'node:net'
import tls from 'node:tls'
import crypto from 'node:crypto'
import {spawnSync} from 'node:child_process'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const q=path.dirname(fileURLToPath(import.meta.url)), hash=x=>crypto.createHash('sha256').update(JSON.stringify(x)).digest('hex'), fileHash=f=>crypto.createHash('sha256').update(fs.readFileSync(f)).digest('hex')
export async function exercisePackagedBrowser({frontend,endpoint,username:adminUser,password:adminPassword,fixtureRoot,candidatePID,binary}){
 const commit='de501586b354e8f93adc5a66984c1c61b48dbf96',platformArch=process.arch==='x64'?'amd64':'arm64'
 const report={test:'WINDOWS_PACKAGED_OUTBOUND_MUTUAL_TLS',sourceCommit:commit,candidateBinarySha256:fileHash(binary),startedAt:new Date().toISOString(),status:'RUNNING',evidenceClass:'HOST_NATIVE_PROVEN',environment:{platform:process.platform,architecture:process.arch,execution:'Actual packaged Windows CLI Go TLS/Trojan outbound; independent Node standard TLS peer and fixed loopback nonce target'},assertions:[],modes:[],privateValuesCaptured:false,projectAdaptedToOS:false,scope:'Normal client/inbound/outbound/config owners, actual TLS12/13 client cert/key text/path, server SPKI, CA/hostname/client-credential/protocol-principal refusals,700ms outbound handshake timeout and restoration. No external ACME/provider/ECH/other platform/generated client-key distribution claim.',cleanup:'PENDING'}
 const check=(n,ok)=>{if(!ok)throw Error(n);report.assertions.push(n)}
 const require=createRequire(path.join(path.resolve(frontend),'package.json')),{request}=require('@playwright/test'),ctx=await request.newContext({baseURL:endpoint,timeout:20000})
 const get=async route=>{const r=await ctx.get(route),j=await r.json();if(!r.ok()||!j.success)throw Error('MTLS_OWNER_GET_FAILED');return j.obj}
 const post=async(route,data)=>{const token=(await get('api/csrf')).token,r=await ctx.post(route,{...data,headers:{Origin:new URL(endpoint).origin,'X-CSRF-Token':token}});return{status:r.status(),...await r.json()}}
 const save=async(object,action,data,initUsers='')=>{const r=await post('api/save',{form:{object,action,data:JSON.stringify(data),initUsers}});if(!r.success)throw Error('MTLS_OWNER_'+object.toUpperCase()+'_SAVE_FAILED')}
 const inventory=async()=>hash({config:(await get('api/config')).config,clients:(await get('api/clients')).clients.map(x=>x.id).sort((a,b)=>a-b),inbounds:(await get('api/inbounds')).inbounds.map(x=>x.tag).sort(),outbounds:(await get('api/outbounds')).outbounds,tls:(await get('api/tls')).tls,services:(await get('api/services')).services})
 const wait=async(fn,code,ms=6000)=>{const deadline=Date.now()+ms;while(!fn()){if(Date.now()>deadline)throw Error(code);await new Promise(r=>setTimeout(r,15))}}
 let original,baseline,held=false,changed=false,clientID,outboundID,privateDir,files=[],material,peer,echo,echoCount=0,successes=0,connections=0
 const allSockets=new Set(),peerRecords=[],timeoutRecords=[]
 const marker='w5-outbound-mtls-'+crypto.randomBytes(5).toString('hex'),clientName=marker+'-client',tags={inbound:marker+'-ingress',outbound:marker+'-egress'},user=clientName,pass=crypto.randomBytes(24).toString('hex'),socksPass=crypto.randomBytes(24).toString('hex')
 const ports={ingress:24871,peer:24872,echo:24873}
 const track=s=>{allSockets.add(s);s.once('close',()=>allSockets.delete(s));s.on('error',()=>{});s.setTimeout(5000,()=>s.destroy())}
 const closePeer=async()=>{if(!peer)return;const old=peer;peer=null;for(const s of allSockets)s.destroy();await new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('SCOPED_PEER_JOIN_DEADLINE')),7000);old.close(()=>{clearTimeout(timer);resolve()})});await wait(()=>allSockets.size===0,'SCOPED_CONNECTION_CLEANUP')}
 const unused=async port=>{const s=net.createServer();try{await new Promise((resolve,reject)=>{s.once('error',reject);s.listen(port,'127.0.0.1',resolve)});return true}finally{if(s.listening)await new Promise(r=>s.close(r))}}
 const startPeer=async(version,silent=false)=>{
  await closePeer()
  if(silent){peer=net.createServer(s=>{track(s);const r={acceptedAt:Date.now(),remoteClose:false};timeoutRecords.push(r);s.resume();s.once('close',()=>{r.closedAfterMs=Date.now()-r.acceptedAt;r.remoteClose=true})})}
  else{
   peer=tls.createServer({key:material.ServerKey,cert:material.ServerCertificate,ca:material.CA,requestCert:true,rejectUnauthorized:true,minVersion:version,maxVersion:version,handshakeTimeout:4000},s=>{
    track(s);if(++connections>32){s.destroy();return}
    const certificate=s.getPeerCertificate(),record={tlsVersion:s.getProtocol(),clientAuthorized:s.authorized,expectedClientCertificate:!!certificate.raw&&crypto.createHash('sha256').update(certificate.raw).digest('hex')===crypto.createHash('sha256').update(new crypto.X509Certificate(material.ClientCertificate).raw).digest('hex'),authenticatedPrincipal:false,nonceDelivered:false};peerRecords.push(record)
    let data=Buffer.alloc(0),accepted=false,upstream
    s.on('data',b=>{
     if(accepted){upstream?.write(b);return}
     data=Buffer.concat([data,b]);if(data.length>1024){s.destroy();return}if(data.length<68)return
     const expected=Buffer.concat([Buffer.from(crypto.createHash('sha224').update(pass).digest('hex')+'\r\n'),Buffer.from([1,1,127,0,0,1,ports.echo>>8,ports.echo&255,13,10])])
     if(!s.authorized||!record.expectedClientCertificate||!data.subarray(0,68).equals(expected)){s.destroy();return}
     record.authenticatedPrincipal=true;accepted=true;upstream=net.createConnection({host:'127.0.0.1',port:ports.echo});track(upstream);upstream.once('connect',()=>{upstream.write(data.subarray(68));data=Buffer.alloc(0)});upstream.on('data',b=>{record.nonceDelivered=true;s.write(b)});upstream.once('end',()=>s.end());s.once('close',()=>upstream.destroy())
    })
   });peer.on('tlsClientError',()=>{})
   peer.on('connection',track)
  }
  await new Promise((resolve,reject)=>{peer.once('error',()=>reject(Error('INDEPENDENT_TLS_PEER_BIND_FAILED')));peer.listen(ports.peer,'127.0.0.1',resolve)})
 }
 const probe=async()=>{
  const s=net.createConnection({host:'127.0.0.1',port:ports.ingress});let b=Buffer.alloc(0),closed=false,wake=null,stage='SOCKS_AUTH'
  s.on('data',x=>{b=Buffer.concat([b,x]);wake?.()});s.on('close',()=>{closed=true;wake?.()});s.on('end',()=>{closed=true;wake?.()});s.on('error',()=>{closed=true;wake?.()})
  const deadline=Date.now()+4000,read=n=>new Promise((resolve,reject)=>{const timer=setTimeout(()=>{wake=null;reject(Error('PROBE_DEADLINE'))},Math.max(1,deadline-Date.now()));const poll=()=>{if(b.length>=n){const v=b.subarray(0,n);b=b.subarray(n);clearTimeout(timer);wake=null;resolve(v)}else if(closed){clearTimeout(timer);wake=null;reject(Error('REMOTE_CLOSE'))}};wake=poll;poll()})
  try{s.write(Buffer.from([5,1,2]));if(!(await read(2)).equals(Buffer.from([5,2])))throw Error('SOCKS_AUTH_METHOD');const u=Buffer.from(user),p=Buffer.from(socksPass);s.write(Buffer.concat([Buffer.from([1,u.length]),u,Buffer.from([p.length]),p]));if(!(await read(2)).equals(Buffer.from([1,0])))throw Error('SOCKS_AUTH_FAILED');stage='CONNECT';s.write(Buffer.from([5,1,0,1,127,0,0,1,ports.echo>>8,ports.echo&255]));const reply=await read(4);if(reply[0]!==5)throw Error('SOCKS_REPLY_INVALID');if(reply[1]!==0)return{nonceMatched:false,remoteDenial:true};if(reply[3]===1)await read(6);else if(reply[3]===4)await read(18);else if(reply[3]===3)await read((await read(1))[0]+2);else throw Error('SOCKS_ADDRESS_INVALID');stage='NONCE';const nonce=crypto.randomBytes(32);s.write(nonce);return{nonceMatched:(await read(32)).equals(nonce),remoteDenial:false}}catch(e){return{nonceMatched:false,remoteDenial:e.message==='REMOTE_CLOSE'&&stage!=='SOCKS_AUTH',code:e.message}}finally{s.destroy()}
 }
 const maintenance=async enabled=>{const s=await get('api/runtime/status'),r=await post('api/runtime/maintenance',{data:{generation:s.status.generation,enabled}});if(!r.success)throw Error('NORMAL_MTLS_MAINTENANCE_FAILED');held=enabled}
 const outbound=(mode,version)=>{
  const options={enabled:true,server_name:material.Name,handshake_timeout:'700ms',min_version:version==='TLSv1.2'?'1.2':'1.3',max_version:version==='TLSv1.2'?'1.2':'1.3'}
  if(mode==='path')Object.assign(options,{certificate_path:path.join(privateDir,'ca.pem'),client_certificate_path:path.join(privateDir,'client.pem'),client_key_path:path.join(privateDir,'client-key.pem')})
  else Object.assign(options,{client_certificate:[material.ClientCertificate],client_key:[material.ClientKey],...(mode==='server-pin'?{certificate_public_key_sha256:[crypto.createHash('sha256').update(new crypto.X509Certificate(material.ServerCertificate).publicKey.export({type:'spki',format:'der'})).digest('base64')]}:{certificate:[material.CA]})})
  return{type:'trojan',tag:tags.outbound,server:'127.0.0.1',server_port:ports.peer,password:pass,connect_timeout:'3s',tls:options,...(outboundID?{id:outboundID}:{})}
 }
 try{
  check('exact_native_windows_marked_disposable_package',process.platform==='win32'&&['x64','arm64'].includes(process.arch)&&Number.isInteger(candidatePID)&&JSON.parse(fs.readFileSync(path.join(q,'fixture-candidate-expectation.json'),'utf8')).commit===commit&&fs.readFileSync(path.join(fixtureRoot,'.w5-disposable'),'utf8').trim()==='W5_SYNTHETIC_ONLY')
  check('normal_authorized_admin_login',(await post('api/login',{form:{user:adminUser,pass:adminPassword}})).success)
  original=(await get('api/config')).config;baseline=await inventory()
  for(const p of Object.values(ports))check('fixed_loopback_port_'+p+'_unused_before_fixture',await unused(p))
  const generated=spawnSync(path.join(q,`naive-portable-echo-windows-${platformArch}.exe`),['generate-private','--controller-memory-only'],{encoding:'utf8',windowsHide:true,timeout:30000,maxBuffer:65536});if(generated.status!==0)throw Error('PRIVATE_MTLS_MATERIAL_FAILED');material=JSON.parse(generated.stdout)
  check('fresh_server_client_private_pairs_match',new crypto.X509Certificate(material.ServerCertificate).checkHost(material.Name)===material.Name&&new crypto.X509Certificate(material.ServerCertificate).checkPrivateKey(crypto.createPrivateKey(material.ServerKey))&&new crypto.X509Certificate(material.ClientCertificate).checkPrivateKey(crypto.createPrivateKey(material.ClientKey)))
  privateDir=path.join(fs.realpathSync(fixtureRoot),'.'+marker);fs.mkdirSync(privateDir)
  const ps=`$ErrorActionPreference='Stop'; try { $w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; $w5Step='CREATE_SECURITY'; [Console]::Out.WriteLine('W5_STAGE=CREATE_SECURITY'); $w5Acl=New-Object System.Security.AccessControl.DirectorySecurity; $w5Acl.SetAccessRuleProtection($true,$false); $w5User=[System.Security.Principal.WindowsIdentity]::GetCurrent().User; $w5Acl.SetOwner($w5User); $w5Principals=@($w5User.Value,'S-1-5-18')|Select-Object -Unique; foreach($w5Value in $w5Principals){$w5Sid=New-Object System.Security.Principal.SecurityIdentifier($w5Value); $w5Rule=New-Object System.Security.AccessControl.FileSystemAccessRule($w5Sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow'); $w5Acl.AddAccessRule($w5Rule)}; $w5Step='WRITE_ACL'; [Console]::Out.WriteLine('W5_STAGE=WRITE_ACL'); $w5Directory=New-Object System.IO.DirectoryInfo($w5Input.path); $w5Directory.SetAccessControl($w5Acl); $w5Step='READ_ACL'; [Console]::Out.WriteLine('W5_STAGE=READ_ACL'); $w5Actual=$w5Directory.GetAccessControl(); $w5Rules=@($w5Actual.GetAccessRules($true,$false,[System.Security.Principal.SecurityIdentifier])); $w5Step='VERIFY_ACL'; [Console]::Out.WriteLine('W5_STAGE=VERIFY_ACL'); if(-not $w5Actual.AreAccessRulesProtected -or $w5Rules.Count -ne $w5Principals.Count -or $w5Actual.GetOwner([System.Security.Principal.SecurityIdentifier]).Value -ne $w5User.Value -or @($w5Rules|Where-Object {$_.AccessControlType -ne 'Allow' -or $_.IdentityReference.Value -notin $w5Principals}).Count -ne 0){throw 'EXACT_PRIVATE_ACL_REQUIRED'}; 'PASS_CURRENT_USER_SYSTEM_ONLY' } catch { [pscustomobject]@{kind='W5_OWNED_ACL_DIAGNOSTIC';step=$w5Step;exceptionType=$_.Exception.GetType().FullName;errorId=$_.FullyQualifiedErrorId;currentUserIsSYSTEM=($w5User.Value -eq 'S-1-5-18');expectedPrincipals=@($w5Principals).Count;actualPrincipals=@($w5Rules).Count;protected=$w5Actual.AreAccessRulesProtected;PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress; exit 1 }`
  const aclStarted=Date.now(),encoded="[Console]::Out.WriteLine('W5_STAGE=PROCESS_STARTED');"+ps
  const acl=spawnSync(path.join(process.env.SystemRoot,'System32/WindowsPowerShell/v1.0/powershell.exe'),['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(encoded,'utf16le').toString('base64')],{stdio:['ignore','pipe','pipe'],env:{...process.env,W5_PRIVATE_METADATA_INPUT:JSON.stringify({path:privateDir})},encoding:'utf8',windowsHide:true,timeout:20000,maxBuffer:65536})
  const aclLines=(acl.stdout??'').trim().split(/\r?\n/),aclStages=aclLines.filter(x=>/^W5_STAGE=[A-Z_]+$/.test(x)),aclOutput=aclLines.filter(x=>!x.startsWith('W5_STAGE=')).join('\n')
  report.metadataCall={transport:'ENVIRONMENT_CLOSED_STDIN',elapsedMs:Date.now()-aclStarted,stages:aclStages}
  if(acl.status!==0||acl.error){let detail;try{detail=JSON.parse(aclOutput)}catch{};report.metadataFailure={exitCode:acl.status,spawnCode:acl.error?.code??null,stderrBytes:Buffer.byteLength(acl.stderr??''),stages:aclStages,...(detail?.kind==='W5_OWNED_ACL_DIAGNOSTIC'?detail:{})}};check('private_client_key_paths_current_user_SYSTEM_only',acl.status===0&&!acl.error&&aclOutput.trim()==='PASS_CURRENT_USER_SYSTEM_ONLY')
  for(const [n,value]of[['ca.pem',material.CA],['client.pem',material.ClientCertificate],['client-key.pem',material.ClientKey]]){const f=path.join(privateDir,n);fs.writeFileSync(f,value,{flag:'wx'});files.push({path:f,sha256:fileHash(f)})}
  echo=net.createServer(s=>{track(s);let b=Buffer.alloc(0);s.on('data',v=>{b=Buffer.concat([b,v]);if(b.length===32){echoCount++;s.end(b)}else if(b.length>32)s.destroy()})});await new Promise((resolve,reject)=>{echo.once('error',reject);echo.listen(ports.echo,'127.0.0.1',resolve)})
  await maintenance(true);await save('clients','new',{name:clientName,enable:true,inbounds:[],config:{socks:{username:user,password:socksPass}},ipLimitMode:'monitor'});clientID=(await get('api/clients')).clients.find(x=>x.name===clientName)?.id
  await save('inbounds','new',{type:'socks',tag:tags.inbound,listen:'127.0.0.1',listen_port:ports.ingress,tls_id:0,addrs:[]},String(clientID));await save('outbounds','new',outbound('text','TLSv1.2'));outboundID=(await get('api/outbounds')).outbounds.find(x=>x.tag===tags.outbound)?.id
  const cfg=structuredClone(original);cfg.route.rules=[{inbound:[tags.inbound],action:'route',outbound:tags.outbound},...(cfg.route.rules??[])];changed=true;await save('config','edit',cfg);await maintenance(false)
  for(const [mode,version]of[['text','TLSv1.2'],['path','TLSv1.3'],['server-pin','TLSv1.3']]){
   await startPeer(version);await save('outbounds','edit',outbound(mode,version));const result=await probe();check(mode+'_actual_packaged_outbound_mutual_TLS_authenticated_nonce',result.nonceMatched);successes++;await wait(()=>echoCount===successes,'EXACT_NONCE_TARGET_ACK')
   const positive=peerRecords.at(-1);check(mode+'_independent_peer_verifies_exact_client_certificate_and_TLS_version',positive?.clientAuthorized&&positive.expectedClientCertificate&&positive.authenticatedPrincipal&&positive.nonceDelivered&&positive.tlsVersion===version)
   const record={mode,version,positive,denials:[]}
   const denials=mode==='server-pin'?['wrong-server-pin','missing-client','wrong-client-CA','wrong-protocol-principal']:['wrong-server-CA','wrong-hostname','missing-client','wrong-client-CA','wrong-protocol-principal']
   for(const kind of denials){const d=outbound(mode,version);if(kind==='wrong-server-pin')d.tls.certificate_public_key_sha256=[Buffer.alloc(32).toString('base64')];if(kind==='wrong-server-CA'){delete d.tls.certificate_path;d.tls.certificate=[material.WrongCertificate]}if(kind==='wrong-hostname')d.tls.server_name='wrong.fixture.invalid';if(kind==='missing-client'){for(const key of ['client_certificate','client_certificate_path','client_key','client_key_path'])delete d.tls[key]}if(kind==='wrong-client-CA'){delete d.tls.client_certificate_path;delete d.tls.client_key_path;d.tls.client_certificate=[material.WrongCertificate];d.tls.client_key=[material.WrongKey]}if(kind==='wrong-protocol-principal')d.password=crypto.randomBytes(24).toString('hex');await save('outbounds','edit',d);const denied=await probe();check(mode+'_'+kind+'_denied_before_nonce',!denied.nonceMatched&&denied.remoteDenial&&echoCount===successes);record.denials.push({kind,...denied})}
   await save('outbounds','edit',outbound(mode,version));check(mode+'_normal_credential_restore_reconnect',(await probe()).nonceMatched);successes++;await wait(()=>echoCount===successes,'RESTORED_NONCE_TARGET_ACK');report.modes.push(record)
  }
  await startPeer('TLSv1.3',true);await save('outbounds','edit',outbound('path','TLSv1.3'));const denied=await probe();await wait(()=>timeoutRecords.length===1&&timeoutRecords[0].remoteClose,'NATIVE_OUTBOUND_TLS_TIMEOUT_CLOSE',2500)
  check('actual_700ms_outbound_handshake_timeout_closes_independent_silent_peer',!denied.nonceMatched&&denied.remoteDenial&&timeoutRecords[0].closedAfterMs>=500&&timeoutRecords[0].closedAfterMs<2000&&echoCount===successes);report.timeout={configuredMs:700,remoteClosedAfterMs:timeoutRecords[0].closedAfterMs,observer:'Independent raw TCP peer; did not apply fixture timeout or send a TLS response'}
  check('current_runtime_available_after_TLS_denials',(await get('api/runtime/status')).status.apiAvailable===true);report.status='PASS'
 }catch(e){report.status='FAIL';report.failure=/^[A-Za-z0-9_.:-]{1,160}$/.test(e.message)?e.message:'OUTBOUND_MTLS_FIXTURE_FAILED'}
 finally{
  try{
   if(original){if(!held)await maintenance(true);if(changed)await save('config','edit',original);for(const row of (await get('api/inbounds')).inbounds.filter(x=>x.tag===tags.inbound))await save('inbounds','del',row.tag);for(const row of (await get('api/clients')).clients.filter(x=>x.name===clientName))await save('clients','del',row.id);for(const row of (await get('api/outbounds')).outbounds.filter(x=>x.tag===tags.outbound))await save('outbounds','del',row.tag);await maintenance(false);check('normal_owner_original_config_inventory_and_runtime_restored',await inventory()===baseline&&(await get('api/runtime/status')).status.apiAvailable===true)}
   await closePeer();if(echo){await new Promise(r=>echo.close(r));echo=null}for(const s of allSockets)s.destroy();await wait(()=>allSockets.size===0,'INDEPENDENT_PEERS_NO_LEAKED_CONNECTIONS')
   for(const port of Object.values(ports))check('scoped_TCP_listener_'+port+'_removed',await unused(port));check('all_denied_requests_excluded_from_independent_nonce_counter',echoCount===successes)
   if(privateDir){if(fs.realpathSync(path.dirname(privateDir))!==fs.realpathSync(fixtureRoot)||fs.lstatSync(privateDir).isSymbolicLink())throw Error('CONFINED_PRIVATE_KEY_CLEANUP_REQUIRED');for(const f of files){if(path.dirname(f.path)!==privateDir||fs.lstatSync(f.path).isSymbolicLink()||fileHash(f.path)!==f.sha256)throw Error('EXACT_OWNED_PRIVATE_FILE_REQUIRED');fs.unlinkSync(f.path)}fs.rmdirSync(privateDir);check('private_client_key_files_removed_after_listener_and_owner_cleanup',!fs.existsSync(privateDir))}
   report.cleanup='NORMAL_ORIGINAL_INVENTORY_CONFIG_RESTORED_ALL_SCOPED_SOCKETS_LISTENERS_JOINED_PRIVATE_CLIENT_KEYS_REMOVED'
  }catch(e){report.status='FAIL';report.cleanup='SCOPED_OUTBOUND_MTLS_RECOVERY_REQUIRED';report.cleanupFailure=/^[A-Za-z0-9_.:-]{1,160}$/.test(e.message)?e.message:'OUTBOUND_MTLS_CLEANUP_FAILED';for(const s of allSockets)s.destroy();peer?.close();echo?.close()}
  await ctx.dispose();report.authenticatedNonceCount=successes;report.independentTargetCount=echoCount;report.finishedAt=new Date().toISOString();fs.writeFileSync(path.join(q,`WINDOWS_OUTBOUND_MTLS_NATIVE_RUNTIME_${process.env.W5_NATIVE_PROFILE}.json`),JSON.stringify(report,null,2)+'\n')
 }
 if(report.status!=='PASS')throw Error(report.failure??report.cleanupFailure??'OUTBOUND_MTLS_QUALIFICATION_FAILED')
 return{status:'PASS',assertions:report.assertions,outboundMTLSReport:`WINDOWS_OUTBOUND_MTLS_NATIVE_RUNTIME_${process.env.W5_NATIVE_PROFILE}.json`}
}
