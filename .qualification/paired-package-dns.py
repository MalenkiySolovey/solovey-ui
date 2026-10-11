"""Goal-owned paired deployed old/current DNS witnesses; no embedded test Box.

Only loopback DNS, SOCKS and HTTP peers are allowed. DB template contains DDL,
never captured row values. Package processes execute independently and their
logs/admin credentials stay inside a private marked fixture.
"""
import argparse, datetime, hashlib, http.server, ipaddress, json, os
from pathlib import Path
import platform, re, select, shutil, socket, socketserver, sqlite3, struct
import subprocess, tempfile, threading, time, uuid

OLD='3523eeea9f91c091ab3f0fbdb94c3955e37798b4'
NEW='ec534be49a9141a45743b035077ebfa5c0148208'
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def exact(s,n):
    b=b''
    while len(b)<n:
        x=s.recv(n-len(b))
        if not x:raise RuntimeError('PEER_EOF')
        b+=x
    return b
def address(s):
    t=exact(s,1)[0]
    if t==1:host=socket.inet_ntoa(exact(s,4))
    elif t==3:host=exact(s,exact(s,1)[0]).decode()
    elif t==4:host=socket.inet_ntop(socket.AF_INET6,exact(s,16))
    else:raise RuntimeError('FIXTURE_ADDRESS_TYPE_UNSUPPORTED')
    return host,struct.unpack('!H',exact(s,2))[0]
def endpoint(host,port):
    if host!='127.0.0.1':raise RuntimeError('FIXTURE_NONLOOPBACK_DESTINATION_DENIED')
    return host,port
def wire_address(host,port):return b'\x01'+socket.inet_aton(host)+struct.pack('!H',port)
def domain(q):
    pos=12;parts=[]
    while q[pos]:n=q[pos];pos+=1;parts.append(q[pos:pos+n].decode());pos+=n
    return '.'.join(parts),pos+5
def question(name):
    return struct.pack('!6H',15151,0x100,1,0,0,0)+b''.join(bytes([len(p)])+p.encode() for p in name.split('.'))+b'\0\0\x01\0\x01'
class UDPServer(socketserver.ThreadingUDPServer):allow_reuse_address=True;daemon_threads=True
class TCPServer(socketserver.ThreadingTCPServer):allow_reuse_address=True;daemon_threads=True
class Peers:
    def __init__(self):
        self.lock=threading.Lock();self.dns=[];self.proxy=[];self.http=0;self.servers=[]
        outer=self
        class DNS(socketserver.BaseRequestHandler):
            def handle(self):
                q,s=self.request;name,end=domain(q)
                with outer.lock:outer.dns.append(name)
                value='127.0.0.1' if name=='dns.fixture.invalid' else '192.0.2.123'
                answer=q[:2]+struct.pack('!5H',0x8180,1,1,0,0)+q[12:end]+b'\xc0\x0c'+struct.pack('!HHIH',1,1,60,4)+socket.inet_aton(value)
                s.sendto(answer,self.client_address)
        self.dns_server=self.start(UDPServer(('127.0.0.1',0),DNS));self.dns_port=self.dns_server.server_address[1]
        class SOCKS(socketserver.BaseRequestHandler):
            def handle(self):
                c=self.request;c.settimeout(8)
                try:
                    if exact(c,1)!=b'\x05':return
                    exact(c,exact(c,1)[0]);c.sendall(b'\x05\0')
                    header=exact(c,3);host,port=address(c)
                    if header[1]==3:
                        u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);u.bind(('127.0.0.1',0));u.settimeout(5)
                        c.sendall(b'\x05\0\0'+wire_address(*u.getsockname()))
                        try:
                            while True:
                                ready,_,_=select.select([c,u],[],[],5)
                                if not ready or c in ready:return
                                data,client=u.recvfrom(65535)
                                if data[:3]!=b'\0\0\0' or data[3]!=1:continue
                                dest=(socket.inet_ntoa(data[4:8]),struct.unpack('!H',data[8:10])[0]);endpoint(*dest)
                                with outer.lock:outer.proxy.append({'network':'udp','port':dest[1]})
                                with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as relay:
                                    relay.settimeout(3);relay.sendto(data[10:],dest);answer,_=relay.recvfrom(65535)
                                u.sendto(b'\0\0\0'+wire_address(*dest)+answer,client)
                        finally:u.close()
                    elif header[1]==1:
                        dest=endpoint(host,port)
                        with outer.lock:outer.proxy.append({'network':'tcp','port':port})
                        with socket.create_connection(dest,timeout=3) as r:
                            c.sendall(b'\x05\0\0'+wire_address('127.0.0.1',0))
                            while True:
                                ready,_,_=select.select([c,r],[],[],5)
                                if not ready:return
                                for src in ready:
                                    chunk=src.recv(65535)
                                    if not chunk:return
                                    (r if src is c else c).sendall(chunk)
                except (OSError,RuntimeError):return
        self.socks_server=self.start(TCPServer(('127.0.0.1',0),SOCKS));self.socks_port=self.socks_server.server_address[1]
        class HTTP(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                with outer.lock:outer.http+=1
                data=b'{"version":3,"rules":[{"domain":["migration.fixture.invalid"]}]}'
                self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
            def log_message(self,*args):pass
        self.http_server=self.start(http.server.ThreadingHTTPServer(('127.0.0.1',0),HTTP));self.http_port=self.http_server.server_address[1]
    def start(self,s):self.servers.append(s);threading.Thread(target=s.serve_forever,daemon=True).start();return s
    def snapshot(self):
        with self.lock:return {'dnsQuestions':list(self.dns),'proxy':list(self.proxy),'httpRequests':self.http}
    def close(self):
        for s in self.servers:s.shutdown();s.server_close()
def free_port():
    with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def query(port,name):
    with socket.create_connection(('127.0.0.1',port),timeout=3) as c:
        c.settimeout(4);c.sendall(b'\x05\x01\0');assert exact(c,2)==b'\x05\0'
        c.sendall(b'\x05\x01\0'+wire_address('192.0.2.53',53));header=exact(c,3)
        if header[:2]!=b'\x05\0':raise RuntimeError('DEPLOYED_TCP_CONNECT_REJECTED')
        address(c)
        q=question(name);c.sendall(struct.pack('!H',len(q))+q)
        answer=exact(c,struct.unpack('!H',exact(c,2))[0]);rcode=answer[3]&15;count=struct.unpack('!H',answer[6:8])[0]
        return {'rcode':rcode,'answer':socket.inet_ntoa(answer[-4:]) if count else None}
def populate(file,schema,config,port,web,sub,proxy):
    db=sqlite3.connect(file)
    for sql in schema['ddl']:db.execute(sql)
    cols=lambda table:{r[1] for r in db.execute('pragma table_info('+table+')')}
    def put(table,values):
        row={k:(v.encode() if k in {'options','addrs','out_json','server','client'} and isinstance(v,str) else v) for k,v in values.items() if k in cols(table)}
        db.execute('insert into '+table+' ('+','.join(row)+') values ('+','.join('?' for _ in row)+')',list(row.values()))
    for key,value in {'version':'2026.3.3','config':json.dumps(config),'webListen':'127.0.0.1','webPort':str(web),'webPath':'/app/','subListen':'127.0.0.1','subPort':str(sub),'timeLocation':'UTC'}.items():put('settings',{'key':key,'value':value})
    put('tls',{'id':0,'name':'none','server':'{}','client':'{}'})
    put('inbounds',{'id':1,'type':'mixed','tag':'w5-dns','tls_id':0,'options':json.dumps({'listen':'127.0.0.1','listen_port':port}),'addrs':'[]','out_json':'{}','sort_order':1})
    put('outbounds',{'id':1,'type':'direct','tag':'direct','options':'{}','sort_order':1})
    put('outbounds',{'id':2,'type':'socks','tag':'proxy','options':json.dumps({'server':'127.0.0.1','server_port':proxy,'version':'5'}),'sort_order':2})
    db.commit();db.close();os.chmod(file,0o600)

def main():
    p=argparse.ArgumentParser();p.add_argument('--old-binary',required=True);p.add_argument('--new-binary',required=True);p.add_argument('--schema',required=True);p.add_argument('--output',required=True);p.add_argument('--base',required=True);p.add_argument('--systemd-policy');p.add_argument('--cases',default='explicit-proxy,implicit-final-proxy,explicit-direct,bootstrap-hostname,fakeip,rcode,direct-ruleset-download');a=p.parse_args()
    report={'test':'PAIRED_DEPLOYED_PACKAGED_DNS_OLD_CURRENT','startedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'oldCommit':OLD,'candidateCommit':NEW,'status':'RUNNING','cases':[],'assertions':[],'privateContentsCaptured':False,'environment':{'os':platform.system(),'release':platform.release(),'arch':platform.machine(),'uid':os.getuid(),'pid1':Path('/proc/1/comm').read_text().strip()},'scope':'Real packaged old/current processes and loopback UDP/SOCKS/HTTP path counters. No custom Box/DNS adapter. Not installed broker/reboot/mDNS/Resolved or external client qualification.'}
    supervised=bool(a.systemd_policy)
    if supervised and (os.getuid()==0 or 'microsoft' in platform.release().lower() or report['environment']['pid1']!='systemd'):raise RuntimeError('ACTUAL_NONROOT_NATIVE_SYSTEMD_REQUIRED')
    schema=json.loads(Path(a.schema).read_text());root=Path(tempfile.mkdtemp(prefix='paired-dns-',dir=a.base));os.chmod(root,0o700);(root/'.w5-owner').write_text(NEW)
    report['privateFixturePath']=str(root);report['oldSchemaSourceSha256']=schema['originalCaptureSha256'];report['schemaTemplateSha256']=sha(a.schema);report['binarySha256']={'old':sha(a.old_binary),'current':sha(a.new_binary)}
    report['queryCarrier']='TCP DNS through real packaged SOCKS ingress; configured upstream UDP DNS and actual SOCKS relay'
    peers=Peers();child=None;unit=None;log=None
    def write():Path(a.output).write_text(json.dumps(report,indent=2)+'\n')
    def check(name,ok):
        if not ok:raise RuntimeError(name)
        report['assertions'].append(name);write()
    def stop():
        nonlocal child,unit,log
        if child:
            if unit:subprocess.run(['sudo','-n','systemctl','stop',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=35)
            else:child.terminate()
            try:child.wait(timeout=35)
            except subprocess.TimeoutExpired:child.kill();child.wait();raise RuntimeError('PACKAGE_STOP_BOUNDED_DRAIN_FAILED')
            code=child.returncode;child=None;unit=None
            if log:log.close();log=None
            return code
    try:
        check('exact_immutable_old_schema_source',schema['sourceCommit']==OLD and schema['originalCaptureSha256']=='aa18846ff00adca7e6c107a9c9e0a7fad1330319170c74e1ebb105d122d9734f')
        properties=[]
        if supervised:
            allowed=r'^(NoNewPrivileges|PrivateTmp|PrivateDevices|Protect\w+|Restrict\w+|LockPersonality|MemoryDenyWriteExecute|SystemCallArchitectures|CapabilityBoundingSet|AmbientCapabilities|UMask|LimitNOFILE|TasksMax|MemoryHigh|MemoryMax|CPUQuota|LogRateLimitIntervalSec|LogRateLimitBurst)='
            properties=[x for x in Path(a.systemd_policy).read_text().splitlines() if re.match(allowed,x)]
            check('canonical_systemd_zero_capabilities_no_new_privileges','NoNewPrivileges=true' in properties and 'CapabilityBoundingSet=' in properties and 'AmbientCapabilities=' in properties)
            report['systemdPolicySha256']=sha(a.systemd_policy)
        for name in a.cases.split(','):
            if name not in ['explicit-proxy','implicit-final-proxy','explicit-direct','bootstrap-hostname','fakeip','rcode','direct-ruleset-download']:raise RuntimeError('UNKNOWN_FIXTURE_CASE')
            port,web,sub=free_port(),free_port(),free_port()
            server={'tag':'main','address':f'udp://127.0.0.1:{peers.dns_port}'}
            if name!='implicit-final-proxy':server['detour']='direct' if name=='explicit-direct' else 'proxy'
            dns={'servers':[server],'final':'main','disable_cache':True}
            if name=='bootstrap-hostname':
                server['address']=f'udp://dns.fixture.invalid:{peers.dns_port}';server['address_resolver']='bootstrap'
                dns['servers'].append({'tag':'bootstrap','address':f'udp://127.0.0.1:{peers.dns_port}','detour':'direct'})
            if name=='fakeip':
                dns['fakeip']={'enabled':True,'inet4_range':'198.18.0.0/15','inet6_range':'fc00::/18'}
                dns['servers'].append({'tag':'fake','address':'fakeip'})
                dns['rules']=[{'domain':['migration.fixture.invalid'],'server':'fake'}]
            if name=='rcode':
                dns['servers'].append({'tag':'deny','address':'rcode://name_error'})
                dns['rules']=[{'domain':['migration.fixture.invalid'],'server':'deny'}]
            route={'final':'proxy','rules':[{'inbound':['w5-dns'],'action':'hijack-dns'}]}
            if name=='direct-ruleset-download':route['rule_set']=[{'type':'remote','tag':'w5-rules','format':'source','url':f'http://127.0.0.1:{peers.http_port}/rules.json','download_detour':'direct'}]
            config={'log':{'level':'debug','output':'stderr'},'dns':dns,'ntp':{'enabled':False},'route':route,'experimental':{}}
            case=root/name;case.mkdir(mode=0o700);data=case/'data';data.mkdir(mode=0o700);(data/'.w5-disposable').write_text('W5_SYNTHETIC_ONLY\n')
            populate(data/'solovey-ui.db',schema,config,port,web,sub,peers.socks_port)
            results=[]
            for revision,binary in [('old',a.old_binary),('current',a.new_binary)]:
                before=peers.snapshot();environment={k:v for k,v in os.environ.items() if not k.startswith(('SUI_','SOLOVEY_'))};environment.update({'SUI_DB_FOLDER':str(data),'SUI_CACHE_FOLDER':str(data),'SUI_STAGING_FOLDER':str(data),'SUI_RUNTIME':str(case/'runtime')})
                log=open(case/(revision+'-private.log'),'wb');os.chmod(log.name,0o600)
                if supervised:
                    unit='solovey-w5-dns-'+uuid.uuid4().hex+'.service'
                    args=['sudo','-n','systemd-run','--quiet','--wait','--pipe','--collect','--unit='+unit,'--property=User='+str(os.getuid()),'--property=Group='+str(os.getgid()),'--property=WorkingDirectory='+str(Path(binary).parent),'--property=ReadWritePaths='+str(root),'--property=TimeoutStopSec=30s']+['--property='+x for x in properties]+['--setenv='+k+'='+environment[k] for k in ['SUI_DB_FOLDER','SUI_CACHE_FOLDER','SUI_STAGING_FOLDER','SUI_RUNTIME']]+[binary]
                else:args=[binary]
                child=subprocess.Popen(args,cwd=Path(binary).parent,env=environment,stdout=log,stderr=log)
                deadline=time.monotonic()+30;answer=None
                while time.monotonic()<deadline:
                    if child.poll() is not None:raise RuntimeError('PACKAGE_EXITED_BEFORE_DATAPLANE_'+name+'_'+revision)
                    try:answer=query(port,'migration.fixture.invalid');break
                    except (OSError,RuntimeError) as error:
                        report['lastQueryFailure']=str(error) if re.fullmatch(r'[A-Za-z0-9_:.-]{1,180}',str(error)) else type(error).__name__
                        time.sleep(.1)
                check(name+'_'+revision+'_actual_dataplane_response',answer is not None)
                if supervised:
                    pid=int(subprocess.check_output(['systemctl','show','--property=MainPID','--value',unit]))
                    status=Path(f'/proc/{pid}/status').read_text()
                    check(name+'_'+revision+'_actual_systemd_binary_nonroot_zero_caps',sha(f'/proc/{pid}/exe')==sha(binary) and re.search(r'^Uid:\s+'+str(os.getuid())+r'\s',status,re.M) and re.search(r'^CapEff:\s+0+$',status,re.M) and re.search(r'^NoNewPrivs:\s+1$',status,re.M))
                after=peers.snapshot();observed={'revision':revision,'answer':answer,'dnsQuestions':after['dnsQuestions'][len(before['dnsQuestions']):],'proxy':after['proxy'][len(before['proxy']):],'httpRequests':after['httpRequests']-before['httpRequests']}
                expected=0 if name in ['explicit-direct','fakeip','rcode'] else 1
                check(name+'_'+revision+'_proxy_path',sum(x['network']=='udp' for x in observed['proxy'])==expected)
                if name=='bootstrap-hostname':check(revision+'_bootstrap_query_observed','dns.fixture.invalid' in observed['dnsQuestions'])
                if name=='fakeip':check(revision+'_fakeip_range_preserved',ipaddress.ip_address(answer['answer']) in ipaddress.ip_network('198.18.0.0/15'))
                elif name=='rcode':check(revision+'_rcode_name_error',answer=={'rcode':3,'answer':None})
                else:check(name+'_'+revision+'_dns_answer',answer=={'rcode':0,'answer':'192.0.2.123'})
                if name=='direct-ruleset-download':check(revision+'_direct_ruleset_http_without_proxy',observed['httpRequests']==1 and all(x['network']!='tcp' for x in observed['proxy']))
                code=stop();check(name+'_'+revision+'_normal_sigterm_exit',code==0)
                results.append(observed)
            check(name+'_paired_answer_equivalence',results[0]['answer']==results[1]['answer'])
            report['cases'].append({'case':name,'results':results});write()
        report['status']='PASS'
    except Exception as error:
        report['status']='FAIL';report['failure']=str(error) if re.fullmatch(r'[A-Za-z0-9_:.-]{1,180}',str(error)) else type(error).__name__
        report['failurePeerCounters']=peers.snapshot()
    finally:
        try:stop()
        finally:peers.close()
        report['finishedAt']=datetime.datetime.now(datetime.timezone.utc).isoformat();write()
        print(json.dumps({'status':report['status'],'cases':len(report['cases']),'assertions':len(report['assertions']),'failure':report.get('failure')}))
    return 0 if report['status']=='PASS' else 1
if __name__=='__main__':raise SystemExit(main())
