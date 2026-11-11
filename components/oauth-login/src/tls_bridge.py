# curl_cffi 传输契约源自 toSub2 8548397e89bf，保留 MIT 许可。
import sys,json,base64
from urllib.parse import urlparse
from curl_cffi import requests
from vendor.sentinel_bridge import sentinel
session=requests.Session(impersonate='chrome146',verify=True)
# 不接受任务提供代理、TLS关闭、地址覆盖或重试参数。
def allowed(url):
    u=urlparse(url)
    return u.scheme=='https' and u.hostname in ('auth.openai.com','chatgpt.com','sentinel.openai.com') and not u.username and not u.password
for line in sys.stdin:
    request_id=None
    try:
        message=json.loads(line);request_id=message['id']
        if message['operation']=='sentinel':
            if not allowed(message['pageUrl']): raise ValueError('origin_invalid')
            result=sentinel(session,message)
        elif message['operation']=='request':
            if not allowed(message['url']): raise ValueError('origin_invalid')
            response=session.request(message['method'],message['url'],headers=dict(message['headers']),data=base64.b64decode(message['body']) if message.get('body') else None,timeout=30,allow_redirects=False)
            cookies=[{'name':c.name,'value':c.value,'domain':c.domain.lstrip('.'),'path':c.path or '/','secure':bool(c.secure),'expires':c.expires*1000 if c.expires else None} for c in session.cookies.jar]
            result={'status':response.status_code,'headers':list(response.headers.multi_items()),'body':base64.b64encode(response.content).decode(),'cookies':cookies}
        else: raise ValueError('operation_invalid')
        reply={'id':request_id,'ok':True,'result':result}
    except Exception:
        reply={'id':request_id,'ok':False,'error_code':'transport_error'}
    sys.stdout.write(json.dumps(reply,separators=(',',':'))+'\n');sys.stdout.flush()
