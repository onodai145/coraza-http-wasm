#!/usr/bin/env python3
"""Bounded local Traefik validation. Generated configs never edit repository config."""
import argparse, collections, concurrent.futures, hashlib, http.client, json, os
from pathlib import Path
import re, shutil, signal, socket, subprocess, sys, threading, time, urllib.parse
import yaml

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT/'testing/load'))
from outcome import finalize_status
PLUGIN = ROOT.parent / 'coraza-http-wasm-traefik'
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--profile',choices=['full','soak'],default='full')
parser.add_argument('--debug',action='store_true',help='Capture DEBUG and 5xx access logs for diagnosis')
args=parser.parse_args()
OUT = ROOT / 'build' / 'load-validation' / time.strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True)
PROJECT = 'coraza-load-' + str(os.getpid())
COMPOSE = OUT / 'compose.json'
STOP = threading.Event()
PHASE = 'setup'
REASONS = []
RESULTS = []
LIMIT = 13 * 1024 * 1024
BASE = {'waf': ('127.0.0.1', 18081), 'baseline': ('127.0.0.1', 18082), 'profile': ('127.0.0.1', 18083)}
SERVICES = ['traefik', 'baseline', 'httpbin']
NAMES = []
SHA = hashlib.sha256((PLUGIN/'build/coraza-http-wasm.wasm').read_bytes()).hexdigest()
if SHA != hashlib.sha256((ROOT/'build/coraza-http-wasm.wasm').read_bytes()).hexdigest():
    raise RuntimeError('WASM source and plugin hashes differ')

def command(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, timeout=kwargs.pop('timeout', 30), **kwargs)

def compose(*args, **kwargs):
    return command('docker', 'compose', '-p', PROJECT, '-f', str(COMPOSE), *args, **kwargs)

def abort(reason):
    first = not STOP.is_set()
    if first:
        REASONS.append(reason)
        print('STOP:', reason, flush=True)
    STOP.set()
    if first and NAMES:
        try:
            command('docker','stop','--time','5',*NAMES,timeout=25)
        except Exception as exc:
            REASONS.append('container stop failed: '+str(exc))

def on_signal(sig, frame):
    abort('interrupted by signal ' + str(sig))

for sig in (signal.SIGINT, signal.SIGTERM):
    signal.signal(sig, on_signal)

source = yaml.safe_load((PLUGIN/'config-dynamic.yaml').read_text())
for variant in ('waf', 'baseline'):
    directory = OUT / variant
    directory.mkdir()
    dynamic = json.loads(json.dumps(source))
    if variant == 'baseline':
        dynamic['http']['routers']['httpbin']['middlewares'].remove('waf')
        del dynamic['http']['middlewares']['waf']
    (directory/'config-dynamic.yaml').write_text(json.dumps(dynamic, indent=2))
    static = {'log': {'level': 'DEBUG' if args.debug else 'WARN'}, 'entryPoints': {'web': {'address': ':80'}},
              'providers': {'file': {'filename': '/etc/traefik/config-dynamic.yaml', 'watch': True}}}
    if args.debug:
        static['accessLog']={'format':'json','filters':{'statusCodes':['500-599']}}
    if variant == 'waf':
        static['api'] = {'insecure': True, 'debug': True, 'dashboard': False}
        static['entryPoints']['traefik'] = {'address': ':8080'}
        static['experimental'] = {'localPlugins': {'coraza': {'moduleName': 'github.com/onodai145/coraza-http-wasm-traefik'}}}
    (directory/'config-static.yaml').write_text(json.dumps(static, indent=2))

logging = {'driver':'json-file','options': {'max-size':'5m','max-file':'2'}}
services = {}
for variant, name in [('waf','traefik'), ('baseline','baseline')]:
    services[name] = {'image':'traefik:v3.6.0', 'cpus':2, 'mem_limit':'2g', 'memswap_limit':'2g',
        'ports':[f'127.0.0.1:{BASE[variant][1]}:80'], 'depends_on':['httpbin'],
        'environment': {'GOMAXPROCS':'2','GOMEMLIMIT':'768MiB','GODEBUG':'gctrace=1'},
        'command':['--configFile=/etc/traefik/config-static.yaml'],
        'volumes':[f'{OUT/variant}:/etc/traefik:ro'], 'logging': logging}
    if variant == 'waf':
        services[name]['ports'].append('127.0.0.1:18083:8080')
        services[name]['volumes'].append(f'{PLUGIN / "build"}:/plugins-local/src/github.com/onodai145/coraza-http-wasm-traefik:ro')
services['httpbin'] = {'image':'mccutchen/go-httpbin:v2.9.0', 'cpus':1, 'mem_limit':'512m', 'memswap_limit':'512m',
    'environment': {'APP_NAME':'httpbin', 'MAX_BODY_SIZE':str(15*1024*1024), 'GOMAXPROCS':'1'},
    'command':['/bin/go-httpbin','-port','8000'], 'logging':logging}
controlled_timeout=os.environ.get('LOAD_BACKEND_HEADER_TIMEOUT')
if controlled_timeout:
    backend_binary=ROOT/'build/load-validation/httpbin-timeout-backend'
    if not backend_binary.is_file():
        raise RuntimeError('build the controlled test backend first')
    services['httpbin']['image']='traefik:v3.6.0'
    services['httpbin']['entrypoint']=['/load-backend']
    services['httpbin']['command']=['-read-header-timeout='+controlled_timeout]
    services['httpbin']['volumes']=[str(backend_binary)+':/load-backend:ro']
COMPOSE.write_text(json.dumps({'services': services}, indent=2))


def memory_bytes(text):
    match = re.fullmatch(r"([0-9.]+)\s*([A-Za-z]+)", text.strip())
    if not match:
        raise ValueError("invalid Docker memory value: "+text)
    value, unit = match.groups()
    factors = {'B':1,'KiB':1024,'MiB':1024**2,'GiB':1024**3,'kB':1000,'MB':1000**2,'GB':1000**3}
    return float(value) * factors[unit]


def monitor():
    with (OUT/'resources.jsonl').open('w') as file:
        while not STOP.is_set():
            try:
                data = command('docker','stats','--no-stream','--format','{{json .}}', *NAMES, timeout=12).stdout
                for line in data.splitlines():
                    record = json.loads(line)
                    used, limit = map(memory_bytes, record['MemUsage'].split('/'))
                    file.write(json.dumps({'time':time.time(),'phase':PHASE,'name':record['Name'],
                        'memory_bytes':used,'memory_limit':limit,'cpu_percent':float(record['CPUPerc'].rstrip('%'))})+'\n')
                    file.flush()
                    if used > limit * .85:
                        abort(f'{record["Name"]} memory exceeded 85% of container limit')
                available = next(int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:'))
                if available < 4*1024**3:
                    abort('host MemAvailable below 4 GiB')
                if shutil.disk_usage(ROOT).free < 10*1024**3:
                    abort('workspace disk free below 10 GiB')
            except Exception as exc:
                abort('resource monitoring failed: '+str(exc))
            STOP.wait(2)

SQLI = '/anything?' + urllib.parse.urlencode({'q':"1' OR '1'='1' --"})
XSS = '/anything?' + urllib.parse.urlencode({'q':'<script>alert(1)</script>'})
SMALL = b'q=' + b'a'*254
MEDIUM = b'q=' + b'a'*(16*1024-2)

class Multipart(bytes):
    def __new__(cls, size):
        boundary = 'coraza-load-boundary-20261107'
        header = ('--'+boundary+'\r\nContent-Disposition: form-data; name="upload"; filename="upload.txt"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()
        trailer = ('\r\n--'+boundary+'--\r\n').encode()
        content = b'a'*(size-len(header)-len(trailer))
        obj = super().__new__(cls, header+content+trailer)
        obj.content_type = 'multipart/form-data; boundary='+boundary
        obj.file_size = len(content)
        obj.file_digest = hashlib.sha256(content).digest()
        return obj


def request(conn, path, body, expected, echo=False):
    start = time.perf_counter()
    conn.request('POST' if body is not None else 'GET', path, body=body,
                 headers={'Host':'localhost','Accept':'application/json,text/html;q=0.9,*/*;q=0.8','User-Agent':'Mozilla/5.0','Content-Type':getattr(body,'content_type','application/x-www-form-urlencoded')})
    response = conn.getresponse()
    data = response.read(64*1024*1024+1)
    if response.status != expected:
        raise AssertionError(f'{path[:60]} status={response.status} expected={expected}, body={data[:160]!r}')
    if len(data) > 64*1024*1024:
        raise AssertionError('response exceeded 64 MiB safety bound')
    if echo:
        result = json.loads(data)
        if isinstance(body,Multipart):
            # Pinned go-httpbin v2.9.0 echoes multipart bytes in data; files is null.
            raw = result.get('data')
            if not isinstance(raw,str):
                raise AssertionError('backend multipart raw data missing')
            got = raw.encode()
            wanted_size = len(body)
            wanted_digest = hashlib.sha256(body).digest()
        else:
            values = (result.get('form') or {}).get('q')
            if not isinstance(values,list) or len(values)!=1:
                raise AssertionError('backend form field missing')
            got = values[0].encode()
            wanted_size = len(body)-2
            wanted_digest = hashlib.sha256(body[2:]).digest()
        if len(got)!=wanted_size or hashlib.sha256(got).digest()!=wanted_digest:
            raise AssertionError('backend body length or hash mismatch')
    return (time.perf_counter()-start)*1000, response.status


def readiness(expected):
    deadline = time.monotonic()+90
    last = None
    while time.monotonic()<deadline and not STOP.is_set():
        conn = http.client.HTTPConnection(*BASE['waf'], timeout=15)
        try:
            request(conn, SQLI, None, expected)
            return
        except Exception as exc:
            last = str(exc)
            STOP.wait(1)
        finally:
            conn.close()
    raise RuntimeError('WAF configuration readiness failed: '+str(last))


def percentile(values, percent):
    ordered = sorted(values)
    return ordered[min(len(ordered)-1, int((len(ordered)-1)*percent))] if ordered else None


def run_case(label, target, concurrency, duration, cases, per_worker_rate=2):
    global PHASE
    if STOP.is_set():
        raise RuntimeError('; '.join(REASONS))
    PHASE = label
    print('START', label, 'concurrency='+str(concurrency), 'seconds='+str(duration), flush=True)
    started = time.monotonic()
    deadline = started+duration
    def worker(index):
        conn = http.client.HTTPConnection(*BASE[target], timeout=120)
        latencies=[]
        statuses=collections.Counter()
        iteration=index
        try:
            while time.monotonic()<deadline and not STOP.is_set():
                tick=time.monotonic()
                path,body,expected,echo=cases[iteration%len(cases)]
                latency,status=request(conn,path,body,expected,echo)
                latencies.append(latency)
                statuses[status]+=1
                iteration+=1
                STOP.wait(max(0, 1/per_worker_rate-(time.monotonic()-tick)))
        except Exception as exc:
            abort(label+': '+str(exc))
        finally:
            conn.close()
        return latencies,statuses
    latencies=[]
    statuses=collections.Counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        for values,counts in pool.map(worker,range(concurrency)):
            latencies.extend(values)
            statuses.update(counts)
    result={'label':label,'target':target,'concurrency':concurrency,'duration_seconds':time.monotonic()-started,
        'requested_seconds':duration,'max_rate_per_worker':per_worker_rate,'count':len(latencies),
        'statuses':dict(statuses),'latency_ms':{'p50':percentile(latencies,.5),'p95':percentile(latencies,.95),
        'p99':percentile(latencies,.99),'max':max(latencies,default=None)},'aborted':STOP.is_set()}
    result['requests_per_second']=result['count']/result['duration_seconds']
    RESULTS.append(result)
    (OUT/'results.json').write_text(json.dumps(RESULTS,indent=2))
    print('RESULT',json.dumps(result),flush=True)
    if STOP.is_set() or not latencies:
        raise RuntimeError('; '.join(REASONS) or 'no requests completed')


def set_engine(engine):
    path=OUT/'waf/config-dynamic.yaml'
    data=json.loads(path.read_text())
    directives=data['http']['middlewares']['waf']['plugin']['coraza']['directives']
    directives[:]=[('SecRuleEngine '+engine) if value.startswith('SecRuleEngine ') else value for value in directives]
    temporary=path.with_suffix('.tmp')
    temporary.write_text(json.dumps(data,indent=2))
    temporary.replace(path)
    readiness(403 if engine=='On' else 200)
    capture_heap('after-mode-'+engine)

def capture_heap(label):
    conn=http.client.HTTPConnection(*BASE['profile'],timeout=15)
    try:
        conn.request('GET','/debug/pprof/heap?debug=1&gc=0')
        response=conn.getresponse()
        data=response.read(4*1024*1024)
        if response.status!=200:
            raise RuntimeError('heap profile HTTP '+str(response.status))
        (OUT/('heap-'+label+'.txt')).write_bytes(data)
    finally:
        conn.close()


state = {'project':PROJECT,'wasm_sha256':SHA,'status':'running','started':time.time(),'reasons':REASONS,
         'rate_note':'bounded offered load; not maximum throughput or production SLA validation',
         'runtime_environment': {'GOMAXPROCS':'2','GOMEMLIMIT':'768MiB','GODEBUG':'gctrace=1'}}
state['profile']=args.profile
state['debug_logging']=args.debug
state['controlled_backend_header_timeout']=controlled_timeout
if controlled_timeout:
    state['backend_binary_sha256']=hashlib.sha256(backend_binary.read_bytes()).hexdigest()
monitor_thread=None
own_project=False
assert memory_bytes("2GiB")==2*1024**3
assert memory_bytes("512 MiB")==512*1024**2
assert memory_bytes("0B")==0
try:
    for _,port in BASE.values():
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',port))
    if compose('ps','-q').stdout.strip():
        raise RuntimeError('project already has containers')
    compose('config','--quiet')
    own_project=True
    compose('up','-d','--pull','never',timeout=60)
    NAMES=compose('ps','-q').stdout.split()
    if len(NAMES)!=3:
        raise RuntimeError('expected exactly three test containers')
    (OUT/'containers-start.json').write_text(command('docker','inspect',*NAMES).stdout)
    monitor_thread=threading.Thread(target=monitor,daemon=True)
    monitor_thread.start()
    readiness(403)
    capture_heap('ready')
    mixed=[('/anything',SMALL,200,True),('/anything',MEDIUM,200,True),(SQLI,None,403,False),(XSS,None,403,False)]
    if args.profile=='full':
        probe_body=b'q='+b'a'*(1024*1024-2)
        run_case('preflight-on-body-1MiB','waf',1,1,[('/anything',probe_body,200,True)],per_worker_rate=1)
        normal=[('/anything',SMALL,200,True)]
        for concurrency in (1,4,8):
            run_case('baseline-c'+str(concurrency),'baseline',concurrency,15,normal)
        for concurrency in (1,4,8):
            run_case('waf-on-normal-c'+str(concurrency),'waf',concurrency,15,normal)
        for concurrency in (1,4,8):
            run_case('waf-on-mixed-c'+str(concurrency),'waf',concurrency,30,mixed)
        for size in (1024*1024,12*1024*1024):
            body=Multipart(size)
            run_case('waf-on-multipart-'+str(size),'waf',1,5,[('/anything',body,200,True)],per_worker_rate=.5)
        body=Multipart(LIMIT)
        run_case('waf-on-body-at-proxy-limit','waf',1,3,[('/anything',body,413,False)],per_worker_rate=.5)
        set_engine('DetectionOnly')
        run_case('waf-detection-body-at-proxy-limit','waf',2,10,[('/anything',body,200,True)],per_worker_rate=.2)
        subprocess.run(['python3',str(PLUGIN/'scripts/body_limits.py'),'--base','http://127.0.0.1:18081',
                        '--limit',str(LIMIT),'--mode','DetectionOnly'],check=True,timeout=90)
        set_engine('On')
    run_case('waf-on-soak-c8','waf',8,600,mixed)
    capture_heap('after-soak')
    PHASE='cooldown'
    print('COOLDOWN 30 seconds',flush=True)
    if STOP.wait(30):
        raise RuntimeError('; '.join(REASONS))
    final=json.loads(command('docker','inspect',*NAMES).stdout)
    (OUT/'containers-end.json').write_text(json.dumps(final,indent=2))
    if any(item['State']['OOMKilled'] or not item['State']['Running'] for item in final):
        raise RuntimeError('container OOM or stopped unexpectedly')
    state['status']='passed'
except BaseException as exc:
    state['status']='failed'
    state['error']=str(exc)
    print('FAILED',str(exc),flush=True)
finally:
    STOP.set()
    if monitor_thread:
        monitor_thread.join(timeout=15)
    try:
        (OUT/'services.log').write_text(compose('logs','--no-color',timeout=30).stdout)
        (OUT/'containers-final.json').write_text(command('docker','inspect',*NAMES).stdout) if NAMES else None
    except Exception as exc:
        state['capture_error']=str(exc)
    try:
        if own_project:
            compose('down','--volumes',timeout=60)
        state['cleanup']='done' if own_project else 'not needed'
    except Exception as exc:
        state['cleanup_error']=str(exc)
        state['status']='failed'
    finalize_status(state, bool(monitor_thread and monitor_thread.is_alive()))
    state['finished']=time.time()
    (OUT/'summary.json').write_text(json.dumps(state,indent=2))
    print('SUMMARY',str(OUT),json.dumps(state),flush=True)
if state['status']!='passed':
    raise SystemExit(1)
