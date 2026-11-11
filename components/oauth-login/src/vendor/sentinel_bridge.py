# SDK 交互核心取自 toSub2 8548397e89bf，保留 MIT 许可。
from __future__ import annotations
import json,subprocess,os
from typing import Any
from urllib.parse import urlparse
from pathlib import Path
from curl_cffi import requests
USER_AGENT='Mozilla/5.0'
SENTINEL_LOADER='https://sentinel.openai.com/backend-api/sentinel/sdk.js'
import re
def request_headers(
    source_url: str,
    target_url: str,
    supplied: dict[str, Any] | None = None,
    user_agent: str = USER_AGENT,
    browser_identity: dict[str, Any] | None = None,
) -> dict[str, str]:
    identity = browser_identity or {}
    source = urlparse(source_url)
    target = urlparse(target_url)
    source_origin = f"{source.scheme}://{source.netloc}"
    target_origin = f"{target.scheme}://{target.netloc}"
    headers = {str(k): str(v) for k, v in (supplied or {}).items()}
    headers.setdefault("user-agent", user_agent)
    headers.setdefault("accept-language", str(identity.get("acceptLanguage") or "zh-CN,zh;q=0.9,en;q=0.8"))
    if identity.get("secChUa"):
        headers.setdefault("sec-ch-ua", str(identity["secChUa"]))
    if identity.get("secChUaMobile"):
        headers.setdefault("sec-ch-ua-mobile", str(identity["secChUaMobile"]))
    if identity.get("secChUaPlatform"):
        headers.setdefault("sec-ch-ua-platform", str(identity["secChUaPlatform"]))
    headers.setdefault("referer", source_url)
    headers.setdefault("accept", "*/*")
    headers.setdefault("sec-fetch-site", "same-origin" if source_origin == target_origin else "cross-site")
    headers.setdefault("sec-fetch-mode", "cors")
    headers.setdefault("sec-fetch-dest", "empty")
    if source_origin == target_origin and target.path.endswith("/req"):
        headers.setdefault("origin", target_origin)
        headers.setdefault("content-type", "text/plain;charset=UTF-8")
    return headers


def fetch_assets(client: requests.Session, page_url: str, user_agent: str = USER_AGENT) -> dict[str, str]:
    loader_response = client.get(
        SENTINEL_LOADER,
        headers={
            "user-agent": user_agent,
            "accept": "*/*",
            "referer": page_url,
            "sec-fetch-dest": "script",
            "sec-fetch-mode": "no-cors",
            "sec-fetch-site": "cross-site",
        },
        timeout=30,
    )
    loader_source = loader_response.text
    if loader_response.status_code != 200:
        raise RuntimeError(f"Sentinel loader HTTP {loader_response.status_code}")
    match = re.search(r"https://sentinel\.openai\.com/sentinel/[^\"']+/sdk\.js", loader_source)
    if not match:
        raise RuntimeError("Sentinel loader did not expose a versioned SDK URL")
    sdk_url = match.group(0)
    sdk_response = client.get(
        sdk_url,
        headers={
            "user-agent": user_agent,
            "accept": "*/*",
            "referer": SENTINEL_LOADER,
            "sec-fetch-dest": "script",
            "sec-fetch-mode": "no-cors",
            "sec-fetch-site": "same-origin",
        },
        timeout=30,
    )
    if sdk_response.status_code != 200:
        raise RuntimeError(f"Sentinel SDK HTTP {sdk_response.status_code}")
    return {
        "loaderUrl": SENTINEL_LOADER,
        "loaderSource": loader_source,
        "sdkUrl": sdk_url,
        "sdkSource": sdk_response.text,
    }


def run_call(
    process: subprocess.Popen[str],
    client: requests.Session | None,
    replay_response: dict[str, Any] | None,
    page_url: str,
    method: str,
    flow: str,
    user_agent: str = USER_AGENT,
    browser_identity: dict[str, Any] | None = None,
) -> dict[str, Any]:
    send(process, {"type": "call", "method": method, "flow": flow})
    assert process.stdout is not None
    network_responses: list[dict[str, Any]] = []
    while True:
        line = process.stdout.readline()
        if not line:
            stderr = ""
            raise RuntimeError(f"Sentinel runtime exited unexpectedly: {stderr[-1000:]}")
        message = json.loads(line)
        if message.get("type") == "request":
            request = message["request"]
            is_req = urlparse(request["url"]).path.endswith("/backend-api/sentinel/req")
            if replay_response is not None and is_req:
                reply = replay_response
            elif replay_response is not None:
                reply = {
                    "status": 200,
                    "url": request["url"],
                    "headers": {"content-type": "text/plain"},
                    "body": "",
                }
            else:
                assert client is not None
                response = client.request(
                    request["method"],
                    request["url"],
                    headers=request_headers(
                        request.get("sourceUrl", page_url),
                        request["url"],
                        request.get("headers"),
                        user_agent,
                        browser_identity,
                    ),
                    data=request.get("body") if request["method"] != "GET" else None,
                    timeout=45,
                    allow_redirects=False,
                )
                reply = {
                    "status": response.status_code,
                    "url": response.url,
                    "headers": dict(response.headers),
                    "body": response.text,
                }
            send(process, {
                "id": request["id"],
                "status": reply["status"],
                "url": reply.get("url", request["url"]),
                "headers": reply.get("headers", {}),
                "body": reply.get("body", ""),
            })
            network_responses.append({
                "method": request["method"],
                "url": request["url"],
                "status": reply["status"],
                "responseBodyLength": len(reply.get("body", "")),
            })
            continue
        if message.get("type") == "result":
            message["networkResponses"] = network_responses
            return message


def send(process, message):
    process.stdin.write(json.dumps(message)+'\n');process.stdin.flush()

def sentinel(client, message):
    device=message['deviceId'];page=message['pageUrl'];ua=message.get('userAgent',USER_AGENT)
    client.cookies.set('oai-did',device,domain='.openai.com',path='/')
    assets=fetch_assets(client,page,ua)
    runtime_input={'url':page,'html':'<!doctype html><html><head></head><body></body></html>','userAgent':ua,'platform':'MacIntel','language':'zh-CN','languages':['zh-CN','zh'],'cookies':{'oai-did':device},'debug':False,'screenWidth':1920,'screenHeight':1080,**assets}
    readfd,writefd=os.pipe()
    process=subprocess.Popen(['node',str(Path(__file__).with_name('sentinel_runtime.cjs')),str(readfd)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,pass_fds=(readfd,))
    os.close(readfd)
    try:
        with os.fdopen(writefd,'w') as stream: json.dump(runtime_input,stream)
        token=run_call(process,client,None,page,'token',message['flow'],ua).get('value')
        if not token: raise ValueError('sentinel_missing')
        return {'token':token}
    finally:
        process.kill();process.wait()
