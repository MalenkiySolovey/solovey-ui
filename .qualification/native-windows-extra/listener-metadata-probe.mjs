// Isolate the mandatory exact-PID/loopback listener assertion. No product runs.
import fs from 'node:fs'
import path from 'node:path'
import net from 'node:net'
import {spawnSync} from 'node:child_process'
const server=net.createServer(s=>s.destroy())
await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve)})
const port=server.address().port,report={test:'NATIVE_ARM_LISTENER_METADATA_RCA',startedAt:new Date().toISOString(),architecture:process.arch,platform:process.platform,productExecuted:false,results:[]}
const script=`$ErrorActionPreference='Stop';[Console]::Out.WriteLine('W5_STAGE=STARTED');$w5Input=$env:W5_METADATA_INPUT|ConvertFrom-Json;$w5Process=Get-Process -Id $w5Input.pid;[Console]::Out.WriteLine('W5_STAGE=PROCESS_FOUND');$w5Rows=@(Get-NetTCPConnection -OwningProcess $w5Process.Id -State Listen|Select-Object LocalAddress,LocalPort);[Console]::Out.WriteLine('W5_STAGE=TABLE_READ');[pscustomobject]@{status='PASS';expectedLoopback=@($w5Rows|Where-Object {$_.LocalAddress -eq '127.0.0.1' -and $_.LocalPort -eq $w5Input.port}).Count -eq 1;allLoopback=@($w5Rows|Where-Object {$_.LocalAddress -notin @('127.0.0.1','::1')}).Count -eq 0;rowCount=$w5Rows.Count;PSVersion=$PSVersionTable.PSVersion.ToString()}|ConvertTo-Json -Compress`
try{
 for(const [name,program]of [['legacy',path.join(process.env.SystemRoot,'System32/WindowsPowerShell/v1.0/powershell.exe')],['native',path.join(process.env.ProgramFiles,'PowerShell/7/pwsh.exe')]]){
  const start=Date.now(),r=spawnSync(program,['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from(script,'utf16le').toString('base64')],{stdio:['ignore','pipe','pipe'],env:{...process.env,W5_METADATA_INPUT:JSON.stringify({pid:process.pid,port})},encoding:'utf8',windowsHide:true,timeout:20000,maxBuffer:65536})
  const lines=(r.stdout??'').trim().split(/\r?\n/);let detail;try{detail=JSON.parse(lines.at(-1))}catch{}
  report.results.push({name,elapsedMs:Date.now()-start,exitCode:r.status,spawnCode:r.error?.code??null,stderrBytes:Buffer.byteLength(r.stderr??''),stages:lines.filter(x=>/^W5_STAGE=[A-Z_]+$/.test(x)),detail:detail??null})
 }
 report.status=report.results.some(x=>x.exitCode===0&&x.detail?.expectedLoopback&&x.detail?.allLoopback)?'PASS':'FAIL'
}finally{await new Promise(resolve=>server.close(resolve));report.cleanup='ONLY_OWNED_LOOPBACK_LISTENER_CLOSED';report.finishedAt=new Date().toISOString();fs.writeFileSync(process.argv[2]??'WINDOWS_LISTENER_METADATA_RCA.json',JSON.stringify(report,null,2)+'\n')}
console.log(JSON.stringify(report));if(report.status!=='PASS')process.exitCode=1
