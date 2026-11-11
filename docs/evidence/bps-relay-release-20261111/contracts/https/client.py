"""仅通过内部 DNS 别名访问测试 Caddy，信任本轮 CA 并校验主机名。"""
import base64
import http.client
import json
import pathlib
import ssl
import time

exchange = pathlib.Path('/exchange')
manifest = json.loads((exchange / 'ready.json').read_text())
expected = base64.b64decode(manifest['png_base64'])
context = ssl.create_default_context(cafile=str(exchange / 'root.crt'))
assert context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED


def request(method, path):
    for attempt in range(40):
        try:
            connection = http.client.HTTPSConnection('relay.example.com', 8443, timeout=3, context=context)
            connection.request(method, path)
            response = connection.getresponse()
            result = response.status, dict(response.getheaders()), response.read()
            connection.close()
            return result
        except OSError:
            if attempt == 39: raise
            time.sleep(.1)


status, headers, body = request('GET', manifest['path'])
assert status == 200 and body == expected and headers['Content-Type'] == 'image/png'
assert headers['Cache-Control'] == 'private, no-store' and headers['X-Content-Type-Options'] == 'nosniff'
print('trusted_ca_hostname_tls_get_original_png=passed http_status=200')
status, headers, body = request('HEAD', manifest['path'])
assert status == 200 and not body and int(headers['Content-Length']) == len(expected)
print('trusted_tls_head_empty_body_content_length=passed http_status=200')
status, _, _ = request('GET', '/api/bps-images/' + 'a' * 43)
assert status == 404
print('unknown_capability_404=passed')
rejected = [('POST', manifest['path']), ('DELETE', manifest['path']), ('OPTIONS', manifest['path']), ('GET', '/api/bps-images/'), ('GET', '/api/bps-images/nested/token'), ('GET', '/'), ('GET', '/state/private-state.txt'), ('GET', '/v8/management/bps/settings')]
for method, path in rejected:
    status, _, body = request(method, path)
    assert status == 404 and b'SYNTHETIC_PRIVATE_STATE' not in body and body != expected
print('methods_directory_nested_state_management_rejected=passed')
print('https_client_exit=0')
