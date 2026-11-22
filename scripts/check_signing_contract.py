import argparse,base64,json,os,pathlib,socket,subprocess,tempfile,time,urllib.request,urllib.error,urllib.parse,uuid
parser=argparse.ArgumentParser(description="Локальный Users -> gateway JWT/JWKS контракт на выделенной БД")
parser.add_argument("--users-bin",required=True)
parser.add_argument("--gateway-bin",required=True)
parser.add_argument("--database",required=True)
args=parser.parse_args()
users_root=str(pathlib.Path(__file__).resolve().parents[1])
gateway_root=users_root
dsn=args.database
database=urllib.parse.urlparse(dsn)
if database.hostname not in ("127.0.0.1","localhost") or not database.path.endswith("_test"):
    raise SystemExit("database must be a dedicated loopback _test database")
def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1",0))
        return sock.getsockname()[1]
users_port,gateway_port=free_port(),free_port()
users_base="http://127.0.0.1:"+str(users_port)
gateway_base="http://127.0.0.1:"+str(gateway_port)
kid="contract-"+uuid.uuid4().hex
logs_dir=pathlib.Path(tempfile.mkdtemp(prefix="ovm-signing-contract-"))
subprocess.run([args.users_bin,'migrate','--dir','migrations','--dsn',dsn,'up'],cwd=users_root,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
common={k:v for k,v in os.environ.items() if not k.startswith(('JWT_','USERS_','BOOTSTRAP_','HTTP_','PORT','MEDIA_','CONTENT_','TASKS_','TASK_HUNTER_','ENTITIES_','FEED_','RECOMMENDATIONS_','ENV','DATABASE_'))}
common.update(USERS_LOGIN_CONTEXT_SECRET='fixture-context-secret-0123456789abcdef',JWT_ISSUER='users',JWT_AUDIENCE='overmindv-gateway')
userenv={**common,'DATABASE_URL':dsn,'HTTP_ADDR':'127.0.0.1:'+str(users_port),'USERS_PROFILE':'local','JWT_SIGNING_SEED':base64.b64encode(bytes([9])*32).decode(),'JWT_SIGNING_KID':kid,'MEDIA_URL':'http://127.0.0.1:9','MEDIA_USERS_TOKEN':'fixture-media'}
gatewayenv={**common,'PORT':str(gateway_port),'JWT_JWKS_URL':users_base+'/.well-known/jwks.json','USERS_GRAPHQL_URL':users_base+'/query','TASK_HUNTER_TOKEN':'fixture-hunter','MEDIA_TOKEN':'fixture-media','FEED_TOKEN':'fixture-feed','MEDIA_URL':'http://127.0.0.1:9','CORS_ORIGINS':'http://localhost:5173'}
logs=[open(logs_dir/'users.log','w'),open(logs_dir/'gateway.log','w')]
processes=[]
def request(url,query=None,variables=None,token=None):
    headers={'Content-Type':'application/json','Origin':'http://localhost:5173'}
    if token: headers['Authorization']='Bearer '+token
    data=json.dumps({'query':query,'variables':variables or {}}).encode() if query else None
    with urllib.request.urlopen(urllib.request.Request(url,data=data,headers=headers),timeout=4) as response:return json.load(response)
try:
    processes=[subprocess.Popen([args.users_bin],cwd=users_root,env=userenv,stdout=logs[0],stderr=logs[0]),subprocess.Popen([args.gateway_bin],cwd=gateway_root,env=gatewayenv,stdout=logs[1],stderr=logs[1])]
    for port in [users_port,gateway_port]:
        for _ in range(40):
            if any(p.poll() is not None for p in processes):
                raise RuntimeError('local service exited; logs: '+str(logs_dir))
            try:
                urllib.request.urlopen('http://127.0.0.1:'+str(port)+'/health',timeout=1)
                break
            except urllib.error.HTTPError:break
            except (urllib.error.URLError,TimeoutError):time.sleep(.2)
        else:raise RuntimeError('local service did not start; inspect sanitized runtime logs')
    jwks=request(users_base+'/.well-known/jwks.json')
    assert len(jwks['keys'])==1 and jwks['keys'][0]['kid']==kid and 'd' not in jwks['keys'][0]
    suffix=uuid.uuid4().hex[:12]
    result=request(users_base+'/query','mutation($input:RegisterInput!){register(input:$input){token refreshToken user{id}}}',{'input':{'email':suffix+'@example.com','username':'c'+suffix,'password':'fixture-password','refreshSession':True}})
    assert not result.get('errors'),'Users register failed'
    payload=result['data']['register'];token=payload['token']
    header=json.loads(base64.urlsafe_b64decode(token.split('.')[0]+'=='))
    assert header['alg']=='EdDSA' and header['kid']==kid
    me=request(gateway_base+'/graphql','{me{id username}}',token=token)
    assert not me.get('errors') and me['data']['me']['id']==payload['user']['id'],'Gateway did not validate Users EdDSA token/session'
    refreshed=request(users_base+'/query','mutation($token:String!){refreshSession(refreshToken:$token){token}}',{'token':payload['refreshToken']})
    assert not refreshed.get('errors'),'Users refresh failed'
    me=request(gateway_base+'/graphql','{me{id}}',token=refreshed['data']['refreshSession']['token'])
    assert not me.get('errors'),'Gateway rejected refreshed asymmetric token'
    print('PASS: live Users JWKS -> gateway verification without JWT_SECRET; durable session + rotated refresh access')
finally:
    for p in processes:
        p.terminate()
    for p in processes:
        try:p.wait(timeout=10)
        except subprocess.TimeoutExpired:p.kill();p.wait()
    for f in logs:f.close()
