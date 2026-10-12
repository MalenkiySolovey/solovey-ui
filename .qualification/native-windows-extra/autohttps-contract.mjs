// Actual packaged Windows panel/subscription listeners. The fixture adapts to
// product settings, TLS and restart owners; no server implementation is copied.
import fs from 'node:fs'
import path from 'node:path'
import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import crypto from 'node:crypto'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
const q=path.dirname(fileURLToPath(import.meta.url)),root=path.resolve(q,'../../..'),digest=f=>crypto.createHash('sha256').update(fs.readFileSync(f)).digest('hex')
const objectHash=x=>crypto.createHash('sha256').update(JSON.stringify(x)).digest('hex')
export async function exercisePackagedBrowser({endpoint,username,password,fixtureRoot,candidatePID,subscriptionEndpoint,binary,environment}){
 const commit='de501586b354e8f93adc5a66984c1c61b48dbf96', name='w5-fixture.invalid',platformArch=process.arch==='x64'?'amd64':'arm64'
 const report={test:'WINDOWS_PACKAGED_PANEL_SUBSCRIPTION_AUTOHTTPS',sourceCommit:commit,candidateBinarySha256:digest(binary),startedAt:new Date().toISOString(),status:'RUNNING',evidenceClass:'HOST_NATIVE_PROVEN',environment:{platform:process.platform,architecture:process.arch,execution:'Actual packaged CLI and normal panel/subscription settings/restart owners'},assertions:[],listeners:[],privateValuesCaptured:false,projectAdaptedToOS:false,scope:'Native Windows AMD64 actual panel and subscription TLS12/13, custom trust/hostname/minimum/Host denial, fragmented TLS and HTTP, complete request then FIN, bounded silent/partial peer and normal owner rollback. No public ACME/provider/account/ECH/other architecture claim.',cleanup:'PENDING'}
 const check=(label,ok)=>{if(!ok)throw Error(label);report.assertions.push(label)}
 const keys=['webCertFile','webKeyFile','webDomain','subCertFile','subKeyFile','subDomain']
 const endpoints={panel:new URL(endpoint),subscription:new URL(subscriptionEndpoint)}
 let material,tlsMode=false,changed=false,original,baseline,generation,cookies=new Map(),certDir,files=[]
 const cert=()=>path.join(certDir,'certificate.pem'),key=()=>path.join(certDir,'key.pem')
 const nativeMetadataEnvironment={...process.env}
 if(process.env.W5_POWERSHELL_BASELINE_FILE){
  const preserved=JSON.parse(fs.readFileSync(process.env.W5_POWERSHELL_BASELINE_FILE,'utf8').replace(/^\uFEFF/,''))
  for(const [key,value]of Object.entries(preserved)){for(const existing of Object.keys(nativeMetadataEnvironment))if(existing.toLowerCase()===key.toLowerCase())delete nativeMetadataEnvironment[existing];if(value!==null)nativeMetadataEnvironment[key]=value}
 }
 const powershell=(script,input,nativeHost=false)=>{
  const started=Date.now(),encoded="[Console]::Out.WriteLine('W5_STAGE=PROCESS_STARTED');"+script
  const program=nativeHost?path.join(process.env.ProgramFiles,'PowerShell/7/pwsh.exe'):path.join(process.env.SystemRoot,'System32/WindowsPowerShell/v1.0/powershell.exe')
  const r=spawnSync(program,['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(encoded,'utf16le').toString('base64')],{stdio:['ignore','pipe','pipe'],env:{...nativeMetadataEnvironment,W5_PRIVATE_METADATA_INPUT:JSON.stringify(input)},encoding:'utf8',windowsHide:true,timeout:20000,maxBuffer:65536})
  const lines=(r.stdout??'').trim().split(/\r?\n/),stages=lines.filter(x=>/^W5_STAGE=[A-Z_]+$/.test(x)),output=lines.filter(x=>!x.startsWith('W5_STAGE=')).join('\n')
  report.metadataCalls??=[];report.metadataCalls.push({transport:'ENVIRONMENT_CLOSED_STDIN',elapsedMs:Date.now()-started,stages})
  if(r.status!==0||r.error){let detail;try{detail=JSON.parse(output)}catch{};report.metadataFailure={exitCode:r.status,spawnCode:r.error?.code??null,stderrBytes:Buffer.byteLength(r.stderr??''),stages,...(detail?.kind==='W5_OWNED_ACL_DIAGNOSTIC'?detail:{})};throw Error('WINDOWS_OWNED_FIXTURE_METADATA_FAILED')};return output.trim()
 }
 const call=(relative,method='GET',data)=>new Promise((resolve,reject)=>{
  const u=new URL(relative,endpoint),host=tlsMode?`${name}:${u.port}`:u.host,body=data?new URLSearchParams(data).toString():null
  const headers={Host:host,Origin:`${tlsMode?'https':'http'}://${host}`,...(cookies.size?{Cookie:[...cookies].map(([k,v])=>`${k}=${v}`).join('; ')}:{}),...(body?{'Content-Type':'application/x-www-form-urlencoded','Content-Length':Buffer.byteLength(body)}:{})}
  if(data?._csrf){headers['X-CSRF-Token']=data._csrf;delete data._csrf}
  const request=(tlsMode?https:http).request({hostname:'127.0.0.1',port:u.port,path:u.pathname+u.search,method,headers,agent:false,...(tlsMode?{ca:material.certificate,servername:name,rejectUnauthorized:true}:{}),timeout:3000},res=>{
   for(const raw of res.headers['set-cookie']??[]){const s=raw.split(';',1)[0],at=s.indexOf('=');if(at>0)cookies.set(s.slice(0,at),s.slice(at+1))}
   let b='';res.on('data',x=>{b+=x;if(b.length>4*1024*1024){request.destroy();reject(Error('BOUNDED_PRIVATE_RESPONSE_REQUIRED'))}})
   res.on('end',()=>{try{const j=JSON.parse(b);resolve({...j,status:res.statusCode})}catch{reject(Error('OWNER_API_JSON_REQUIRED'))}})
  });request.on('timeout',()=>request.destroy(Error('OWNER_API_DEADLINE')));request.on('error',()=>reject(Error('OWNER_API_TRANSPORT_UNAVAILABLE')));if(body)request.write(body);request.end()
 })
 const get=async relative=>{const r=await call(relative);if(r.status!==200||!r.success)throw Error('AUTHENTICATED_OWNER_GET_FAILED');return r.obj}
 const post=async(relative,data={})=>{const csrf=await get('api/csrf');const r=await call(relative,'POST',{...data,_csrf:csrf.token});if(r.status!==200||!r.success)throw Error('AUTHENTICATED_OWNER_POST_FAILED');return r.obj}
 const login=async()=>{cookies=new Map();const r=await post('api/login',{user:username,pass:password});if(r?.state!=='authenticated')throw Error('NORMAL_OWNER_LOGIN_REQUIRED')}
 const inventory=async()=>objectHash({config:(await get('api/config')).config,clients:(await get('api/clients')).clients.map(x=>x.id).sort((a,b)=>a-b),inbounds:(await get('api/inbounds')).inbounds.map(x=>x.tag).sort(),tls:(await get('api/tls')).tls,services:(await get('api/services')).services})
 const save=async data=>post('api/save',{object:'settings',action:'edit',data:JSON.stringify(data)})
 const restart=async()=>{await post('api/restartApp');cookies=new Map()}
 const peer=(which,mode,quiet=false)=>{
  const r=spawnSync(path.join(q,`autohttps-windows-peer-${platformArch}.exe`),[mode],{input:JSON.stringify({Target:`127.0.0.1:${endpoints[which].port}`,Name:name,Authority:name,Certificate:material.certificate}),encoding:'utf8',windowsHide:true,timeout:20000,maxBuffer:65536})
  let v;try{v=JSON.parse(r.stdout)}catch{if(quiet)return null;throw Error('WINDOWS_WIRE_PEER_FRAME_REQUIRED')}
  if(r.status!==0||v.status!=='PASS'){if(quiet)return null;report.peerFailure={listener:which,...v};throw Error(/^[A-Z0-9_]{1,160}$/.test(v.failure??'')?v.failure:'WINDOWS_WIRE_PEER_FAILED')}
  return v
 }
 const awaitState=async(mode,previous)=>{
  const deadline=Date.now()+45000;let last
  while(Date.now()<deadline){
   try{tlsMode=mode;await login();const s=await get('api/runtime/status');if(s.status.apiAvailable&&s.status.generation!==previous){const settings=await get('api/settings');if(mode?keys.every(k=>settings[k]===(k.endsWith('CertFile')?cert():k.endsWith('KeyFile')?key():name)):keys.every(k=>settings[k]===original[k]))return s.status.generation}}catch(e){last=e.message}
   await new Promise(r=>setTimeout(r,100))
  }
  report.readinessLastFailure=last??'OWNER_GENERATION_OR_SETTINGS_NOT_READY';throw Error('NORMAL_APP_RESTART_READINESS_DEADLINE')
 }
 const unused=async port=>{const s=net.createServer();try{await new Promise((resolve,reject)=>{s.once('error',reject);s.listen(Number(port),'127.0.0.1',resolve)});return true}finally{if(s.listening)await new Promise(r=>s.close(r))}}
 try{
  check('native_windows_amd64_exact_marked_fixture',process.platform==='win32'&&['x64','arm64'].includes(process.arch)&&JSON.parse(fs.readFileSync(path.join(q,'fixture-candidate-expectation.json'),'utf8')).commit===commit&&Number.isInteger(candidatePID)&&fs.readFileSync(path.join(fixtureRoot,'.w5-disposable'),'utf8').trim()==='W5_SYNTHETIC_ONLY')
  for(const v of Object.values(endpoints))check('owned_endpoint_remains_loopback',v.protocol==='http:'&&v.hostname==='127.0.0.1'&&Number(v.port)>0)
  await login();baseline=await inventory();const settings=await get('api/settings');original=Object.fromEntries(keys.map(k=>[k,settings[k]]));generation=(await get('api/runtime/status')).status.generation
  check('normal_settings_begin_without_TLS_override',keys.every(k=>original[k]==='')&&settings.webListen==='127.0.0.1'&&settings.subListen==='127.0.0.1')
  const generated=spawnSync(path.join(q,`fixture-tls-material-windows-${platformArch}.exe`),['--private-controller'],{encoding:'utf8',windowsHide:true,timeout:30000,maxBuffer:65536})
  if(generated.status!==0)throw Error('PRIVATE_CONTROLLED_CERTIFICATE_GENERATION_FAILED');material=JSON.parse(generated.stdout)
  const x509=new crypto.X509Certificate(material.certificate);check('fresh_matching_private_certificate_and_key',x509.checkHost(name)===name&&x509.checkPrivateKey(crypto.createPrivateKey(material.key))&&Date.parse(x509.validTo)>Date.now()+48*3600000)
  report.certificateSHA256=material.certificateSha256
  certDir=path.join(fs.realpathSync(fixtureRoot),'.w5-autohttps-'+crypto.randomBytes(6).toString('hex'));fs.mkdirSync(certDir)
  const acl=powershell(`$ErrorActionPreference='Stop'; try { $w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; $w5Step='CREATE_SECURITY'; [Console]::Out.WriteLine('W5_STAGE=CREATE_SECURITY'); $w5Acl=New-Object System.Security.AccessControl.DirectorySecurity; $w5Acl.SetAccessRuleProtection($true,$false); $w5User=[System.Security.Principal.WindowsIdentity]::GetCurrent().User; $w5Acl.SetOwner($w5User); $w5Principals=@($w5User.Value,'S-1-5-18')|Select-Object -Unique; foreach($w5Value in $w5Principals){$w5Sid=New-Object System.Security.Principal.SecurityIdentifier($w5Value); $w5Rule=New-Object System.Security.AccessControl.FileSystemAccessRule($w5Sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow'); $w5Acl.AddAccessRule($w5Rule)}; $w5Step='WRITE_ACL'; [Console]::Out.WriteLine('W5_STAGE=WRITE_ACL'); $w5Directory=New-Object System.IO.DirectoryInfo($w5Input.path); $w5Directory.SetAccessControl($w5Acl); $w5Step='READ_ACL'; [Console]::Out.WriteLine('W5_STAGE=READ_ACL'); $w5Actual=$w5Directory.GetAccessControl(); $w5Rules=@($w5Actual.GetAccessRules($true,$false,[System.Security.Principal.SecurityIdentifier])); $w5Step='VERIFY_ACL'; [Console]::Out.WriteLine('W5_STAGE=VERIFY_ACL'); if(-not $w5Actual.AreAccessRulesProtected -or $w5Rules.Count -ne $w5Principals.Count -or $w5Actual.GetOwner([System.Security.Principal.SecurityIdentifier]).Value -ne $w5User.Value -or @($w5Rules|Where-Object {$_.AccessControlType -ne 'Allow' -or $_.IdentityReference.Value -notin $w5Principals}).Count -ne 0){throw 'EXACT_PRIVATE_ACL_REQUIRED'}; 'PASS_CURRENT_USER_SYSTEM_ONLY' } catch { [pscustomobject]@{kind='W5_OWNED_ACL_DIAGNOSTIC';step=$w5Step;exceptionType=$_.Exception.GetType().FullName;errorId=$_.FullyQualifiedErrorId;currentUserIsSYSTEM=($w5User.Value -eq 'S-1-5-18');expectedPrincipals=@($w5Principals).Count;actualPrincipals=@($w5Rules).Count;protected=$w5Actual.AreAccessRulesProtected;PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress; exit 1 }`,{path:certDir})
  check('private_certificate_directory_explicit_current_user_SYSTEM_ACL',acl==='PASS_CURRENT_USER_SYSTEM_ONLY')
  for(const [file,value]of[[cert(),material.certificate],[key(),material.key]]){fs.writeFileSync(file,value,{flag:'wx'});files.push({file,sha256:digest(file)})}
  changed=true;await save({webCertFile:cert(),webKeyFile:key(),webDomain:name,subCertFile:cert(),subKeyFile:key(),subDomain:name});await restart();const tlsGeneration=await awaitState(true,generation)
  check('normal_APP_restart_recreates_TLS_listeners_and_fences_core_generation',tlsGeneration!==generation)
  for(const which of ['panel','subscription']){
   const proof=peer(which,'probe');report.listeners.push({listener:which,port:Number(endpoints[which].port),...proof});report.assertions.push(...proof.assertions.map(x=>which+'_'+x))
  }
  const pid=powershell(`$ErrorActionPreference='Stop';$w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; $w5Process=Get-Process -Id $w5Input.pid; @(Get-NetTCPConnection -OwningProcess $w5Process.Id -State Listen|Select-Object LocalAddress,LocalPort)|ConvertTo-Json -Compress`,{pid:candidatePID},process.arch==='arm64')
  const listeners=[JSON.parse(pid)].flat();check('exact_same_panel_process_loopback_listeners_only',listeners.length>=3&&listeners.every(x=>['127.0.0.1','::1'].includes(x.LocalAddress))&&Object.values(endpoints).every(v=>listeners.some(x=>x.LocalPort===Number(v.port))))
  report.status='PASS'
 }catch(e){report.status='FAIL';report.failure=/^[A-Za-z0-9_.:-]{1,160}$/.test(e.message)?e.message:'WINDOWS_AUTOHTTPS_FIXTURE_FAILED'}
 finally{
  try{
   if(changed){const previous=(await get('api/runtime/status')).status.generation;await save(original);await restart();const restored=await awaitState(false,previous);check('six_original_TLS_domain_settings_restored_through_normal_owner',restored!==previous);check('original_config_client_inbound_TLS_service_inventory_restored',await inventory()===baseline)}
   if(certDir){if(fs.realpathSync(path.dirname(certDir))!==fs.realpathSync(fixtureRoot)||fs.lstatSync(certDir).isSymbolicLink())throw Error('CONFINED_CERTIFICATE_CLEANUP_REQUIRED');for(const f of files){if(path.dirname(f.file)!==certDir||fs.lstatSync(f.file).isSymbolicLink()||digest(f.file)!==f.sha256)throw Error('EXACT_OWNED_PRIVATE_FILE_REQUIRED');fs.unlinkSync(f.file)}fs.rmdirSync(certDir);check('private_certificate_and_key_removed_after_owner_rollback',!fs.existsSync(certDir))}
   report.cleanup='NORMAL_ORIGINAL_SETTINGS_LISTENERS_INVENTORY_RESTORED_PRIVATE_KEYS_REMOVED'
  }catch(e){report.status='FAIL';report.cleanup='SCOPED_WINDOWS_OWNER_RECOVERY_REQUIRED';report.cleanupFailure=/^[A-Za-z0-9_.:-]{1,160}$/.test(e.message)?e.message:'WINDOWS_OWNER_RECOVERY_FAILED'}
  report.finishedAt=new Date().toISOString();fs.writeFileSync(path.join(q,`WINDOWS_AUTOHTTPS_PANEL_SUBSCRIPTION_RUNTIME_${process.env.W5_NATIVE_PROFILE}.json`),JSON.stringify(report,null,2)+'\n')
 }
 if(report.status!=='PASS')throw Error(report.failure??report.cleanupFailure??'WINDOWS_AUTOHTTPS_QUALIFICATION_FAILED')
 return{status:'PASS',assertions:report.assertions,autoHTTPSReport:`WINDOWS_AUTOHTTPS_PANEL_SUBSCRIPTION_RUNTIME_${process.env.W5_NATIVE_PROFILE}.json`}
}
