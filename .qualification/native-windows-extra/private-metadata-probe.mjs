// Harness RCA only: PHY-04/06 private fixture permissions must pass before TLS
// settings change. Existing raw failure cannot distinguish console EOF from ACL.
// Compare the original stdin call with environment input and the unchanged ACL.
// No product binary, credential, TLS setting or host policy is touched.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import crypto from 'node:crypto'
import {spawnSync} from 'node:child_process'
const report={test:'WINDOWS_PRIVATE_METADATA_HARNESS_RCA',startedAt:new Date().toISOString(),platform:process.platform,architecture:process.arch,node:process.version,fixture:process.env.W5_FIXTURE_COMMIT??null,productSource:'de501586b354e8f93adc5a66984c1c61b48dbf96',productExecuted:false,privateValuesCaptured:false,results:[],status:'RUNNING'}
if(process.platform!=='win32')throw Error('NATIVE_WINDOWS_REQUIRED')
const program=path.join(process.env.SystemRoot,'System32/WindowsPowerShell/v1.0/powershell.exe')
const preserved=process.env.W5_POWERSHELL_BASELINE_FILE?JSON.parse(fs.readFileSync(process.env.W5_POWERSHELL_BASELINE_FILE,'utf8').replace(/^\uFEFF/,'')):null
if(preserved){
 report.nativeEnvironmentDifferences=Object.entries(preserved).filter(([key,value])=>value!==process.env[key]).map(([key,value])=>({key,preservedBytes:Buffer.byteLength(value??''),currentBytes:Buffer.byteLength(process.env[key]??''),preservedSha256:crypto.createHash('sha256').update(value??'').digest('hex'),currentSha256:crypto.createHash('sha256').update(process.env[key]??'').digest('hex')}))
}
const invoke=(name,script,input,viaEnv)=>{
 const nativeEnv={...process.env,...(preserved??{}),W5_PRIVATE_METADATA_INPUT:JSON.stringify(input)}
 const started=Date.now(),r=spawnSync(program,['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(script,'utf16le').toString('base64')],{env:nativeEnv,encoding:'utf8',windowsHide:true,timeout:20000,maxBuffer:65536,...(viaEnv?{stdio:['ignore','pipe','pipe']}:{input:JSON.stringify(input)})})
 let detail;try{detail=JSON.parse(r.stdout?.trim())}catch{}
 report.results.push({name,elapsedMs:Date.now()-started,exitCode:r.status,spawnCode:r.error?.code??null,stderrBytes:Buffer.byteLength(r.stderr??''),detail:detail??null})
 console.log(JSON.stringify(report.results.at(-1)))
 return r.status===0&&!r.error&&detail?.status==='PASS'
}
if(process.argv[3]==='input-only'){
 const ok=invoke('environment_input',"$ErrorActionPreference='Stop'; $w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; if($w5Input.probe -ne 'W5_METADATA_ONLY'){throw 'INPUT_REQUIRED'}; [pscustomobject]@{status='PASS';stage='READ_ENVIRONMENT_INPUT';PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress",{probe:'W5_METADATA_ONLY'},true)
 report.status=ok?'PASS':'FAIL';report.finishedAt=new Date().toISOString();fs.writeFileSync(process.argv[2],JSON.stringify(report,null,2)+'\n');process.exit(ok?0:1)
}
const directory=fs.mkdtempSync(path.join(os.tmpdir(),'w5-private-metadata-'))
try{
 invoke('original_console_stdin',"$ErrorActionPreference='Stop'; $w5Input=[Console]::In.ReadToEnd()|ConvertFrom-Json; if($w5Input.probe -ne 'W5_METADATA_ONLY'){throw 'INPUT_REQUIRED'}; [pscustomobject]@{status='PASS';stage='READ_CONSOLE_INPUT'}|ConvertTo-Json -Compress",{probe:'W5_METADATA_ONLY'},false)
 const environment=invoke('environment_input',"$ErrorActionPreference='Stop'; $w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; if($w5Input.probe -ne 'W5_METADATA_ONLY'){throw 'INPUT_REQUIRED'}; [pscustomobject]@{status='PASS';stage='READ_ENVIRONMENT_INPUT';PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress",{probe:'W5_METADATA_ONLY'},true)
 const aclScript=`$ErrorActionPreference='Stop'; $w5Step='READ_INPUT'; try {
 $w5Input=$env:W5_PRIVATE_METADATA_INPUT|ConvertFrom-Json; $w5Step='CREATE_SECURITY'; $w5Acl=New-Object System.Security.AccessControl.DirectorySecurity; $w5Acl.SetAccessRuleProtection($true,$false); $w5User=[System.Security.Principal.WindowsIdentity]::GetCurrent().User; $w5Acl.SetOwner($w5User); $w5Principals=@($w5User.Value,'S-1-5-18')|Select-Object -Unique;
 foreach($w5Value in $w5Principals){$w5Sid=New-Object System.Security.Principal.SecurityIdentifier($w5Value); $w5Rule=New-Object System.Security.AccessControl.FileSystemAccessRule($w5Sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow'); $w5Acl.AddAccessRule($w5Rule)};
 $w5Step='WRITE_ACL'; $w5Directory=New-Object System.IO.DirectoryInfo($w5Input.path); $w5Directory.SetAccessControl($w5Acl); $w5Step='READ_ACL'; $w5Actual=$w5Directory.GetAccessControl(); $w5Rules=@($w5Actual.GetAccessRules($true,$false,[System.Security.Principal.SecurityIdentifier])); $w5Step='VERIFY_ACL';
 if(-not $w5Actual.AreAccessRulesProtected -or $w5Rules.Count -ne $w5Principals.Count -or $w5Actual.GetOwner([System.Security.Principal.SecurityIdentifier]).Value -ne $w5User.Value -or @($w5Rules|Where-Object {$_.AccessControlType -ne 'Allow' -or $_.IdentityReference.Value -notin $w5Principals}).Count -ne 0){throw 'EXACT_PRIVATE_ACL_REQUIRED'};
 [pscustomobject]@{status='PASS';stage=$w5Step;currentUserIsSYSTEM=($w5User.Value -eq 'S-1-5-18');expectedPrincipals=@($w5Principals).Count;actualPrincipals=@($w5Rules).Count;protected=$w5Actual.AreAccessRulesProtected;PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress
 } catch { [pscustomobject]@{status='FAIL';stage=$w5Step;exceptionType=$_.Exception.GetType().FullName;errorId=$_.FullyQualifiedErrorId}|ConvertTo-Json -Compress; exit 1 }`;
 invoke('original_console_exact_private_acl',aclScript.replace('$env:W5_PRIVATE_METADATA_INPUT','[Console]::In.ReadToEnd()'),{path:directory},false)
 const acl=invoke('environment_exact_private_acl',aclScript,{path:directory},true)
 report.status=environment&&acl?'PASS':'FAIL';report.sourceSha256=crypto.createHash('sha256').update(fs.readFileSync(new URL(import.meta.url))).digest('hex')
}finally{
 fs.rmdirSync(directory);report.cleanup='ONLY_EMPTY_OWNED_DIRECTORY_REMOVED';report.finishedAt=new Date().toISOString()
 fs.writeFileSync(process.argv[2]??'WINDOWS_PRIVATE_METADATA_HARNESS_RCA.json',JSON.stringify(report,null,2)+'\n')
}
if(report.status!=='PASS')process.exitCode=1
