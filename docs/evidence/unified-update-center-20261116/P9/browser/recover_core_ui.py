import json
from pathlib import Path
from urllib.parse import urlsplit
from patchright.sync_api import sync_playwright

out = Path('/browser')
base = 'http://unified-update-manager-browser-20261116:18317'
original = json.loads((out / 'core-confirmed.json').read_text())['observed_text']
operation_id = original.split('任务编号：', 1)[1].split('\n', 1)[0].strip()
events = []
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path='/opt/google/chrome/chrome', headless=True,
        args=['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking', '--disable-sync', '--disable-extensions'])
    context = browser.new_context(viewport={'width': 1440, 'height': 1000})
    page = context.new_page()
    page.on('response', lambda r: events.append({'method': r.request.method, 'path': urlsplit(r.url).path, 'status': r.status}) if '/v8/' in r.url else None)
    page.goto(base + '/')
    page.get_by_placeholder('Enter the admin key from the startup log').fill('synthetic-admin-only')
    page.get_by_role('button', name='Login', exact=True).click()
    page.wait_for_url(lambda u: '#/login' not in u, timeout=20000)
    page.goto(base + '/management.html#/system')
    page.wait_for_function("document.querySelector('[data-core-update-state]')?.getAttribute('data-core-update-state') === 'success'", timeout=15000)
    text = page.locator('[data-core-update-state]').inner_text()
    assert operation_id in text, '页面未恢复同一操作ID'
    assert 'v1.2.3' in text, '终态未显示目标版本'
    assert not any(e['method'] != 'GET' for e in events), '恢复读取产生了写请求'
    assert not any(e['status'] >= 400 for e in events), '新增管理读取返回错误'
    page.screenshot(path=str(out / 'core-completed.png'), full_page=True)
    receipt = {'operation_id': operation_id, 'observed_text': text, 'same_operation_after_manager_restart': True,
        'read_only_recovery': True, 'requests': events, 'authentication': '合成管理员；未记住凭据时页面重载需要重新登录', 'rc': 0}
    (out / 'core-recovery-result.json').write_text(json.dumps(receipt, ensure_ascii=False, indent=2))
    print(json.dumps(receipt, ensure_ascii=False))
    browser.close()
