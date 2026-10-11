"""Goal ARM adaptation of the accepted AMD64 owner/API lifecycle fixture.
Fresh DDL-only legacy seed is migrated by the actual sealed ARM image.
Actual sealed Docker image, separate migrated synthetic-state copy.

Normal UserService enrolment happens before deployment on only a marked copy.
All runtime/component mutations use authenticated HTTP owners. The private
HTTP worker is captured by this controller, never invoked as a tool directly.
"""
import datetime
import hashlib
import http.client
import http.cookies
import http.server
import json
import os
import platform
from pathlib import Path
import secrets
import signal
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.parse

EVIDENCE=Path(os.environ['W5_PUBLIC_EVIDENCE']).resolve()
COMMIT='ec534be49a9141a45743b035077ebfa5c0148208'
IMAGE='sha256:eb8f1d36f015872f9a12065bd1cedb03dd846fe3bb7b4a53f23dd7807474eecd'
IMAGE_ID=IMAGE
BASE=Path(os.environ['W5_SYNTHETIC_BASE']).resolve()
PRIOR=None
ENROLL=Path(os.environ['W5_ENROLL_HELPER']).resolve()
ORIGINAL=Path(__file__).resolve().parent/'paired-dns-old-schema.json'
ORIGINAL_SHA='564aa92587ed751835ff8f8d78ff4518d879b443e26469780615157752f48f67'
REPORT=EVIDENCE/'DOCKER_ARM64_SYNTHETIC_OLD_MAINTENANCE_COMPONENTS.json'

def sha(p):
    h=hashlib.sha256()
    with open(p,'rb') as f:
        for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
    return h.hexdigest()

def invoke(args,timeout=40,stdin=None):
    return subprocess.run(args,input=stdin,capture_output=True,timeout=timeout,check=False)

def docker(*args,timeout=40):
    r=invoke(['docker',*args],timeout)
    if r.returncode:raise RuntimeError('DOCKER_OPERATION_FAILED_'+args[0])
    return r.stdout

def snap(file):
    c=sqlite3.connect(file.as_uri()+'?mode=ro',uri=True)
    try:
        result={'integrity':c.execute('PRAGMA quick_check').fetchone()[0],
                'schema':c.execute("SELECT value FROM settings WHERE key='coreSchemaVersion'").fetchone()[0],
                'rows':{t:c.execute('SELECT count(*) FROM "'+t+'"').fetchone()[0] for t in ['clients','inbounds','outbounds','tls','services']}}
        for table in ['tls','outbounds','inbounds','services','clients']:
            rows=c.execute('SELECT * FROM "'+table+'" ORDER BY id').fetchall()
            result[table+'LogicalSha256']=hashlib.sha256(json.dumps(rows,sort_keys=True,default=lambda v:bytes(v).hex()).encode()).hexdigest()
        return result
    finally:c.close()

def http_worker():
    request=json.load(sys.stdin)
    port=int(request['port'])
    if not 1024<=port<=65535 or not request['path'].startswith('/') or len(request['path'])>4096:raise RuntimeError('HTTP_PRIVATE_TARGET_INVALID')
    c=http.client.HTTPConnection('127.0.0.1',port,timeout=12)
    try:
        c.request(request['method'],request['path'],request.get('body'),request.get('headers',{}))
        r=c.getresponse()
        raw=r.read(2*1024*1024+1)
        if len(raw)>2*1024*1024:raise RuntimeError('HTTP_PRIVATE_RESPONSE_BOUND')
        try:dto=json.loads(raw)
        except (ValueError,UnicodeError):dto=None
        print(json.dumps({'status':r.status,'cookies':r.headers.get_all('Set-Cookie') or [],'dto':dto}))
    finally:c.close()

def ruleset_worker():
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):
            body=b'{"version":3,"rules":[]}\n'
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.send_header('Content-Length',str(len(body)))
            self.end_headers()
            self.wfile.write(body)
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
    server.daemon_threads=True
    signal.signal(signal.SIGTERM,lambda *_:sys.exit(0))
    print(json.dumps({'event':'READY','loopback':True,'port':server.server_address[1]}),flush=True)
    try:server.serve_forever(poll_interval=.1)
    finally:server.server_close()

if '--private-http' in sys.argv:
    http_worker()
    sys.exit(0)
if '--controlled-ruleset' in sys.argv:
    ruleset_worker()
    sys.exit(0)

report={'test':'SEALED_NATIVE_ARM64_DOCKER_SYNTHETIC_OLD_SCHEMA_MAINTENANCE_COMPONENT_LIFECYCLE',
        'commit':COMMIT,'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'environment':{'platform':'linux','executionKind':'ACTUAL_DOCKER_ON_NATIVE_ARM64_HOST','dockerArchitecture':'arm64','uid':65532},
        'evidenceClass':'HOST_NATIVE_PROVEN',
        'scope':'Reused sealed native ARM64 full image; fresh synthetic legacy rows from immutable old-capture DDL, actual candidate first-boot migration, then private stopped copy and normal maintenance/config/component/process restart. Normal API binds an explicit cache path and controlled loopback source-ruleset peer. Not the identical captured AMD64 DB, old-process/DNS path equivalence, default-cache PASS, installed systemd/reboot, compiled Core/absent profile or carrier/provider proof.',
        'status':'RUNNING','assertions':[],'privateContentsCaptured':False,'sourceChanged':False,'panelPrivilegesChanged':False}
container=None
private=None
cookies={}
port=None
web_path=None
peer=None
legacy_config=None
controlled_config=None

def check(name,ok):
    if not ok:raise RuntimeError(name)
    report['assertions'].append(name)
    REPORT.write_text(json.dumps(report,indent=2)+'\n')

def inspect():return json.loads(docker('inspect',container))[0]

def request(route,method='GET',body=None,form=False,csrf=True):
    if method!='GET' and csrf:
        token=request('api/csrf')['dto']['obj']['token']
    else:token=None
    headers={'User-Agent':'Solovey-W5-Controlled-Fixture','Origin':'http://127.0.0.1:'+str(port)}
    if cookies:headers['Cookie']='; '.join(k+'='+v for k,v in cookies.items())
    if token:headers['X-CSRF-Token']=token
    if form:
        encoded=urllib.parse.urlencode(body)
        headers['Content-Type']='application/x-www-form-urlencoded'
    elif body is not None:
        encoded=json.dumps(body)
        headers['Content-Type']='application/json'
    else:encoded=None
    pid=inspect()['State']['Pid']
    r=invoke(['nsenter','--target',str(pid),'--net','--','python3',str(Path(__file__).resolve()),'--private-http'],
             timeout=20,stdin=json.dumps({'port':port,'method':method,'path':web_path+route,'body':encoded,'headers':headers}).encode())
    if r.returncode:raise RuntimeError('PRIVATE_HTTP_TRANSPORT_FAILED')
    result=json.loads(r.stdout)
    for raw in result['cookies']:
        jar=http.cookies.SimpleCookie()
        jar.load(raw)
        for name,value in jar.items():
            if value.value:cookies[name]=value.value
            else:cookies.pop(name,None)
    return result

def get(route):
    r=request(route)
    if r['status']!=200 or not r['dto'] or not r['dto'].get('success'):raise RuntimeError('AUTHENTICATED_GET_FAILED_'+route.replace('/','_'))
    return r['dto']['obj']

def post(route,value,form=False):
    r=request(route,'POST',value,form)
    return r

def save(obj,value):
    r=post('api/save',{'object':obj,'action':'edit','data':json.dumps(value)},True)
    check('normal_'+obj+'_save_accepted',r['status']==200 and r['dto'] and r['dto'].get('success') is True)

def wait(held,timeout=45):
    deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        try:
            value=get('api/runtime/status')
            if value.get('maintenance')==held and value['status']['apiAvailable'] is (not held):return value
        except (OSError,RuntimeError,KeyError,TypeError):pass
        time.sleep(.25)
    raise RuntimeError('NORMAL_OWNER_RUNTIME_READY_BARRIER_FAILED')

def normal_login(username,password):
    r=post('api/login',{'user':username,'pass':password},True)
    check('normal_synthetic_password_session_authenticated',r['status']==200 and r['dto'] and r['dto'].get('success') and r['dto'].get('obj',{}).get('state')=='authenticated')

def component_state(component):
    return next((c for c in get('api/components')['components'] if c['id']==component),None)

def restart():
    stop_peer()
    docker('stop','--time','10',container,timeout=25)
    check('normal_SIGTERM_exit_zero',inspect()['State']['ExitCode']==0)
    docker('start',container)

def stop_peer():
    global peer
    if peer:
        peer.terminate()
        peer.wait(timeout=8)
        check('controlled_network_peer_stopped_and_joined',peer.returncode in (0,-signal.SIGTERM))
        peer=None

def seed_old_schema():
    global PRIOR,container,port,web_path
    check('native_ARM64_host_root_controlled_fixture',os.getuid()==0 and platform.machine()=='aarch64')
    check('bounded_injected_private_base',str(BASE).startswith('/run/solovey-w5-docker-arm-') and BASE.name=='private' and BASE.is_dir() and not BASE.is_symlink() and BASE.stat().st_uid==0 and BASE.stat().st_mode&0o777==0o700)
    template=json.loads(ORIGINAL.read_text())
    check('immutable_old_DDL_only_no_captured_private_rows',template['sourceCommit']=='3523eeea9f91c091ab3f0fbdb94c3955e37798b4' and template['originalCaptureSha256']=='aa18846ff00adca7e6c107a9c9e0a7fad1330319170c74e1ebb105d122d9734f' and all(sql.startswith(('CREATE TABLE ','CREATE INDEX ','CREATE UNIQUE INDEX ')) for sql in template['ddl']))
    image=json.loads(docker('image','inspect',IMAGE))[0]
    check('sealed_native_ARM64_image_before_seed',image['Id']==IMAGE_ID and image['Architecture']=='arm64' and image['Config']['Labels']['org.opencontainers.image.revision']==COMMIT)
    PRIOR=Path(tempfile.mkdtemp(prefix='solovey-w5-old-schema-ec534-',dir=BASE))
    os.chmod(PRIOR,0o700)
    (PRIOR/'.w5-owned-private-fixture').write_text(COMMIT+'\n')
    data=PRIOR/'data';data.mkdir(mode=0o700);os.chown(data,65532,65532)
    db=sqlite3.connect(data/'solovey-ui.db')
    for sql in template['ddl']:db.execute(sql)
    cols=lambda table:{r[1] for r in db.execute('pragma table_info('+table+')')}
    def put(table,values):
        row={k:(v.encode() if k in {'options','addrs','out_json','server','client'} and isinstance(v,str) else v) for k,v in values.items() if k in cols(table)}
        db.execute('insert into '+table+' ('+','.join(row)+') values ('+','.join('?' for _ in row)+')',list(row.values()))
    config={'log':{'level':'info'},'dns':{'servers':[{'tag':'fixture-dns','address':'udp://127.0.0.1:53353','detour':'direct'}],'final':'fixture-dns'},'route':{'final':'direct','rule_set':[{'tag':'w5-control','type':'remote','format':'source','url':'https://unbound.fixture.invalid/w5.json','download_detour':'direct'}]},'experimental':{'cache_file':{'enabled':True}}}
    for key,value in {'version':'2026.3.3','coreSchemaVersion':'1.11','config':json.dumps(config),'webListen':'127.0.0.1','webPort':'2095','webPath':'/app/','subListen':'127.0.0.1','subPort':'2096','timeLocation':'UTC'}.items():put('settings',{'key':key,'value':value})
    put('tls',{'id':0,'name':'none','server':'{}','client':'{}'})
    put('inbounds',{'id':1,'type':'mixed','tag':'w5-lifecycle','tls_id':0,'options':json.dumps({'listen':'127.0.0.1','listen_port':24881}),'addrs':'[]','out_json':'{}','sort_order':1})
    put('outbounds',{'id':1,'type':'direct','tag':'direct','options':'{}','sort_order':1})
    db.commit();db.close();os.chmod(data/'solovey-ui.db',0o600);os.chown(data/'solovey-ui.db',65532,65532)
    report['syntheticOldInput']={'schemaSourceCommit':template['sourceCommit'],'originalDDLSourceCaptureSha256':template['originalCaptureSha256'],'DDLTemplateSha256':sha(ORIGINAL),'freshSyntheticDatabaseSha256':sha(data/'solovey-ui.db'),'coreSchema':'1.11','capturedOriginalRowValuesUsed':False}
    container=docker('run','--detach','--name',PRIOR.name,'--label','solovey.goal=stage2-w5','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--network','none','--tmpfs','/run/solovey-ui:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532','--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532','--mount','type=bind,src='+str(data)+',dst=/data','-e','SUI_SERVER_PROTECTION_RUNTIME_ROOT=/run/solovey-ui/server-protection',IMAGE).decode().strip()
    port=2095;web_path='/app/'
    deadline=time.monotonic()+70
    while time.monotonic()<deadline:
        try:
            if request('api/csrf')['status']==200:break
        except (RuntimeError,OSError,ValueError):pass
        time.sleep(.25)
    else:raise RuntimeError('ACTUAL_LEGACY_SEED_FIRST_BOOT_PANEL_BARRIER_FAILED')
    docker('stop','--time','10',container,timeout=25)
    check('actual_old_schema_first_boot_normal_shutdown_zero',inspect()['State']['ExitCode']==0)
    log=invoke(['docker','logs',container],timeout=10)
    (PRIOR/'first-boot-log.private').write_bytes(log.stdout+log.stderr);os.chmod(PRIOR/'first-boot-log.private',0o600)
    docker('rm',container);container=None;cookies.clear()
    migrated=snap(data/'solovey-ui.db')
    check('actual_candidate_first_boot_migrated_legacy_schema_to_1_12',migrated['integrity']=='ok' and migrated['schema']=='1.12')
    report['firstBootStoppedMigratedDatabaseSha256']=sha(data/'solovey-ui.db')
    report['firstBootMigratedLogicalState']=migrated

def perform():
    global container,private,port,web_path,peer,legacy_config,controlled_config
    check('immutable_public_old_DDL_template_exact',sha(ORIGINAL)==ORIGINAL_SHA)
    seed_old_schema()
    check('retained_marked_private_migrated_source_owned',PRIOR.parent.resolve()==BASE.resolve() and not PRIOR.is_symlink() and PRIOR.stat().st_uid==0 and PRIOR.stat().st_mode&0o777==0o700 and (PRIOR/'.w5-owned-private-fixture').read_text().strip()==COMMIT)
    original_db=PRIOR/'data/solovey-ui.db'
    baseline=snap(original_db)
    report['retainedSourcePreimageSha256']=sha(original_db)
    report['retainedSourceAfterSeedStopHash']=report['firstBootStoppedMigratedDatabaseSha256']
    prior={'afterSecondStart':{'rows':report['firstBootMigratedLogicalState']['rows']}}
    check('retained_migrated_source_schema_integrity_and_prior_logical_counts',baseline['schema']=='1.12' and baseline['integrity']=='ok' and baseline['rows']==prior['afterSecondStart']['rows'])
    image=json.loads(docker('image','inspect',IMAGE))[0]
    report['candidate']={'imageId':image['Id'],'architecture':image['Architecture']}
    check('exact_sealed_full_image_unchanged',image['Id']==IMAGE_ID and image['Architecture']=='arm64' and image['Config']['User']=='65532:65532' and image['Config']['Labels']['org.opencontainers.image.revision']==COMMIT)
    for entry in (PRIOR/'data').rglob('*'):
        check('source_copy_entry_regular_and_not_symlink',not entry.is_symlink() and (entry.is_file() or entry.is_dir()))
    private=Path(tempfile.mkdtemp(prefix='solovey-w5-closure-ec534-',dir=BASE))
    os.chmod(private,0o700)
    (private/'.w5-owned-private-fixture').write_text(COMMIT+'\n')
    data=private/'data'
    shutil.copytree(PRIOR/'data',data)
    for entry in [data,*data.rglob('*')]:
        os.chown(entry,65532,65532)
        os.chmod(entry,0o700 if entry.is_dir() else 0o600)
    (data/'.w5-disposable').write_text('W5_SYNTHETIC_ONLY\n')
    os.chown(data/'.w5-disposable',65532,65532)
    os.chmod(data/'.w5-disposable',0o600)
    check('new_private_copy_exact_before_synthetic_enrolment',sha(data/'solovey-ui.db')==report['retainedSourcePreimageSha256'])
    username='w5-captured-'+secrets.token_hex(4)
    password='W5!'+secrets.token_urlsafe(30)
    env=dict(os.environ,SUI_DB_FOLDER=str(data))
    r=subprocess.run([str(ENROLL)],input=json.dumps({'Root':str(data),'Username':username,'Password':password}).encode(),capture_output=True,timeout=45,env=env)
    (private/'enrolment-log.private').write_bytes(r.stdout+r.stderr)
    os.chmod(private/'enrolment-log.private',0o600)
    check('normal_existing_UserService_synthetic_enrolment_on_only_copy',r.returncode==0 and b'PASS_NORMAL_SYNTHETIC_USER_OWNER_ENROLMENT' in r.stdout)
    check('original_legacy_TLS_and_network_entity_rows_preserved_after_enrolment',snap(data/'solovey-ui.db')==baseline)
    conn=sqlite3.connect(data/'solovey-ui.db')
    settings=dict(conn.execute("SELECT key,value FROM settings WHERE key IN ('webPort','webPath','webCertFile','webKeyFile')"))
    conn.close()
    port=int(settings.get('webPort','2095'))
    web_path=settings.get('webPath','/')
    if not web_path.endswith('/'):web_path+='/'
    check('inherited_HTTP_management_fixture_listening_shape_supported',not settings.get('webCertFile') and not settings.get('webKeyFile') and 1024<=port<=65535 and web_path.startswith('/'))
    name=private.name
    container=docker('run','--detach','--name',name,'--label','solovey.goal=stage2-w5','--label','solovey.goal.commit='+COMMIT,
                     '--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--network','none',
                     '--tmpfs','/run/solovey-ui:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532',
                     '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=16m,mode=0700,uid=65532,gid=65532',
                     '--mount','type=bind,src='+str(data)+',dst=/data',
                     '-e','SUI_SERVER_PROTECTION_RUNTIME_ROOT=/run/solovey-ui/server-protection',IMAGE).decode().strip()
    deadline=time.monotonic()+60
    while time.monotonic()<deadline:
        try:
            r=request('api/csrf')
            if r['status']==200 and r['dto'] and r['dto'].get('success'):break
        except (RuntimeError,OSError,ValueError):pass
        time.sleep(.25)
    else:raise RuntimeError('INHERITED_PANEL_READY_BARRIER_FAILED')
    topology=inspect()
    check('actual_hardened_network_none_no_host_publication',topology['Config']['User']=='65532:65532' and topology['HostConfig']['NetworkMode']=='none' and not topology['HostConfig']['PortBindings'] and topology['HostConfig']['ReadonlyRootfs'] and 'ALL' in topology['HostConfig']['CapDrop'] and 'no-new-privileges' in topology['HostConfig']['SecurityOpt'])
    process=docker('exec',container,'cat','/proc/1/status').decode()
    check('actual_product_UID65532_zero_caps_NNP1','Uid:\t65532\t65532\t65532\t65532' in process and 'CapEff:\t0000000000000000' in process and 'NoNewPrivs:\t1' in process)
    normal_login(username,password)
    legacy_config=get('api/config')['config']
    if isinstance(legacy_config,str):legacy_config=json.loads(legacy_config)
    report['legacyConfigSha256']=hashlib.sha256(json.dumps(legacy_config,sort_keys=True).encode()).hexdigest()
    baseline_state=get('api/runtime/status')
    report['inheritedRuntimeState']={k:baseline_state['status'][k] for k in ['state','apiAvailable','generation']}
    report['inheritedRelativeCacheFailure']='READ_ONLY_DEFAULT_CWD_REPRODUCTION_PRESERVED_IN_HISTORY'
    check('rescue_panel_authenticated_while_inherited_relative_cache_cannot_start',baseline_state['status']['apiAvailable'] is False and baseline_state['status']['state']=='stopped_by_error')
    pid=inspect()['State']['Pid']
    peer=subprocess.Popen(['nsenter','--target',str(pid),'--net','--','python3',str(Path(__file__).resolve()),'--controlled-ruleset'],
                          stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
    ready=json.loads(peer.stdout.readline())
    check('controlled_loopback_ruleset_peer_ready_before_runtime_apply',ready['event']=='READY' and ready['loopback'] and 1024<=ready['port']<=65535)
    controlled_config=json.loads(json.dumps(legacy_config))
    cache=controlled_config.setdefault('experimental',{}).setdefault('cache_file',{})
    cache['path']='/data/w5-controlled-cache.db'
    remote=[s for s in controlled_config.get('route',{}).get('rule_set',[]) if s.get('type')=='remote']
    check('owned_controlled_bindings_preserve_enabled_cache_and_remote_ruleset_inventory',cache.get('enabled') is True and len(remote)==1)
    for rule in remote:
        rule['url']='http://127.0.0.1:'+str(ready['port'])+'/w5-fixture.json'
        rule['format']='source'
    save('config',controlled_config)
    running=wait(False)
    initial=running['status']['generation']
    report['initialGeneration']=initial
    check('migrated_volume_normal_core_available',running['status']['state']=='running' and bool(initial))
    inventory=get('api/components')
    report['componentInventory']=[{k:c[k] for k in ['id','installed','enabled','active']} for c in inventory['components']]
    check('actual_binary_profile_full',inventory['binaryProfile']=='full')
    fallback=component_state('fallback-html')
    check('existing_fallback_component_installed_with_truthful_initial_state',fallback is not None and fallback['installed'] and fallback['enabled']==fallback['active'])
    report['initialFallbackEnabled']=fallback['enabled']
    save('settings',{'fallback-html.enabled':'true'})
    check('existing_component_normal_enable_dispatch_active',component_state('fallback-html')['active'] is True)
    original_config=controlled_config
    changed=json.loads(json.dumps(original_config))
    changed.setdefault('log',{})['timestamp']=not changed.get('log',{}).get('timestamp',False)
    r=post('api/runtime/maintenance',{'generation':initial,'enabled':True})
    check('normal_generation_bound_maintenance_accepted',r['status']==200 and r['dto'].get('success'))
    held=wait(True)
    check('actual_maintenance_core_stopped_and_private_api_unavailable',held['status']['state']=='stopped')
    save('config',changed)
    check('changed_normal_config_save_cannot_resume_held_core',wait(True)['maintenance'] is True)
    save('settings',{'fallback-html.enabled':'false'})
    f=component_state('fallback-html')
    check('normal_component_disabled_inventory_truthful',f['installed'] and not f['enabled'] and not f['active'])
    r=request('api/components/fallback-html/health')
    check('disabled_component_handler_refused_by_existing_owner',r['status']==409 and r['dto'].get('success') is False)
    restart()
    cookies.clear()
    deadline=time.monotonic()+45
    while time.monotonic()<deadline:
        try:
            if request('api/csrf')['status']==200:break
        except (RuntimeError,OSError,ValueError):pass
        time.sleep(.25)
    normal_login(username,password)
    held=wait(True)
    report['heldRestartGeneration']=held['status']['generation']
    check('same_migrated_volume_maintenance_survives_normal_process_restart',held['maintenance'] and not held['status']['apiAvailable'])
    f=component_state('fallback-html')
    check('component_disable_persists_after_normal_process_restart',f['installed'] and not f['enabled'] and not f['active'])
    r=request('api/components/fallback-html/health')
    check('disabled_component_route_remains_closed_after_restart',r['status']==409 and r['dto'].get('success') is False)
    observed=get('api/config')['config']
    if isinstance(observed,str):observed=json.loads(observed)
    check('changed_config_durable_under_maintenance_restart',observed.get('log',{}).get('timestamp')==changed['log']['timestamp'])
    r=post('api/runtime/maintenance',{'generation':initial,'enabled':False})
    check('old_generation_resume_refused_and_hold_preserved',r['status']==200 and r['dto'].get('success') is False and r['dto'].get('obj',{}).get('reason')=='stale_generation' and wait(True)['maintenance'])
    save('config',original_config)
    save('settings',{'fallback-html.enabled':'true'})
    f=component_state('fallback-html')
    check('existing_component_reenabled_during_hold',f['installed'] and f['enabled'] and f['active'])
    r=request('api/components/fallback-html/health')
    check('reenabled_component_route_dispatch_restored',r['status']==200 and r['dto'] is not None and r['dto'].get('success') is True)
    current=wait(True)
    r=post('api/runtime/maintenance',{'generation':current['status']['generation'],'enabled':False})
    check('current_generation_normal_resume_accepted',r['status']==200 and r['dto'].get('success') is True)
    running=wait(False)
    report['resumedGeneration']=running['status']['generation']
    check('resume_activates_new_generation_and_private_core_available',running['status']['apiAvailable'] and running['status']['generation']!=initial)
    restart()
    cookies.clear()
    deadline=time.monotonic()+45
    while time.monotonic()<deadline:
        try:
            if request('api/csrf')['status']==200:break
        except (RuntimeError,OSError,ValueError):pass
        time.sleep(.25)
    normal_login(username,password)
    running=wait(False)
    check('resumed_state_and_component_enable_persist_after_second_restart',running['maintenance'] is False and component_state('fallback-html')['active'])
    restored=get('api/config')['config']
    if isinstance(restored,str):restored=json.loads(restored)
    check('original_config_restored_through_normal_owner',restored==original_config)
    r=post('api/runtime/maintenance',{'generation':running['status']['generation'],'enabled':True})
    check('normal_final_hold_before_original_legacy_config_restoration',r['status']==200 and r['dto'].get('success'))
    wait(True)
    save('config',legacy_config)
    restored=get('api/config')['config']
    if isinstance(restored,str):restored=json.loads(restored)
    check('original_captured_legacy_config_restored_under_safe_normal_hold',restored==legacy_config and wait(True)['maintenance'])
    save('settings',{'fallback-html.enabled':str(report['initialFallbackEnabled']).lower()})
    check('original_effective_component_enabled_state_restored',component_state('fallback-html')['enabled']==report['initialFallbackEnabled'])
    log=docker('logs',container)
    (private/'product-log.private').write_bytes(log)
    os.chmod(private/'product-log.private',0o600)
    check('product_logs_do_not_expose_synthetic_password',password.encode() not in log)
    docker('stop','--time','10',container,timeout=25)
    check('final_normal_SIGTERM_shutdown_zero',inspect()['State']['ExitCode']==0)
    result=snap(data/'solovey-ui.db')
    check('all_original_legacy_entity_rows_and_TLS_material_preserved_after_lifecycle',result==baseline)
    report['copiedStoppedDatabaseSha256']=sha(data/'solovey-ui.db')
    check('retained_source_volume_exactly_unchanged',sha(original_db)==report['retainedSourcePreimageSha256'] and snap(original_db)==baseline)
    check('immutable_public_DDL_source_still_exact',sha(ORIGINAL)==ORIGINAL_SHA)
    report['status']='PASS'

try:perform()
except Exception as error:
    report['status']='FAILED_UNCLASSIFIED_HARNESS_OR_PRODUCT'
    text=str(error)
    report['failure']=text if text.replace('_','').isalnum() and len(text)<160 else type(error).__name__
finally:
    if peer:
        try:stop_peer()
        except Exception:report['peerCleanup']='VERIFY_REQUIRED'
    if container:
        try:
            raw=invoke(['docker','logs',container],timeout=10)
            ((private or PRIOR)/'final-product-log.private').write_bytes(raw.stdout+raw.stderr)
            os.chmod((private or PRIOR)/'final-product-log.private',0o600)
            invoke(['docker','stop','--time','10',container],timeout=25)
            removed=invoke(['docker','rm',container],timeout=15)
            report['containerCleanup']='OWNED_CONTAINER_REMOVED' if removed.returncode==0 else 'OWNED_CONTAINER_RETAINED'
        except Exception:report['containerCleanup']='VERIFY_REQUIRED'
    report['privateFixtureRetained']=bool(private)
    report['privateFixturePath']=str(private) if private else None
    report['enrolmentHelperSha256']=sha(ENROLL) if ENROLL.exists() else None
    report['controllerSha256']=sha(Path(__file__))
    report['fixtureCommit']=os.environ.get('W5_FIXTURE_COMMIT')
    report['finishedAt']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    REPORT.write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'status':report['status'],'assertions':len(report['assertions']),'failure':report.get('failure'),'report':REPORT.name}))

sys.exit(0 if report['status']=='PASS' and report.get('containerCleanup')=='OWNED_CONTAINER_REMOVED' and report.get('peerCleanup')!='VERIFY_REQUIRED' else 1)
