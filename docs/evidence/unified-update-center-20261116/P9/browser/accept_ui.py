import json
import os
import time
from pathlib import Path
from urllib.parse import urlsplit

from patchright.sync_api import sync_playwright

out = Path('/browser')
base = os.environ.get('PREVIEW_URL', 'http://10.253.16.2:18317')
stage = os.environ.get('ACCEPT_STAGE', 'zip')
events = []
checks = []

def check(name, ok):
    checks.append({'name': name, 'pass': bool(ok)})
    if not ok:
        raise AssertionError(name)

def save():
    (out / (stage + '-result.json')).write_text(json.dumps({'base': base, 'stage': stage, 'checks': checks, 'requests': events}, ensure_ascii=False, indent=2))

with sync_playwright() as p:
    browser = p.chromium.launch(executable_path='/opt/google/chrome/chrome', headless=True,
        args=['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking', '--disable-sync', '--disable-extensions'])
    context = browser.new_context(viewport={'width': 1440, 'height': 1000})
    page = context.new_page()
    page.on('response', lambda r: events.append({'method': r.request.method, 'path': urlsplit(r.url).path, 'status': r.status}) if '/v8/' in r.url else None)
    try:
        page.goto(base + '/', wait_until='domcontentloaded')
        page.get_by_placeholder('Enter the admin key from the startup log').fill('synthetic-admin-only')
        page.get_by_role('button', name='Login', exact=True).click()
        page.wait_for_url(lambda u: '#/login' not in u, timeout=20000)
        page.goto(base + '/management.html#/' + ('plugins' if stage == 'zip' else 'system'))
        if stage == 'zip':
            page.get_by_role('button', name='上传 ZIP', exact=True).wait_for()
            page.wait_for_timeout(500)
            check('插件维护状态无读取错误', '最近插件维护状态尚未确认' not in page.locator('body').inner_text())
            page.screenshot(path=str(out / 'zip-page.png'), full_page=True)
            def upload():
                page.get_by_role('button', name='上传 ZIP', exact=True).click()
                page.get_by_label('选择 ZIP 文件', exact=True).set_input_files('/fixtures/synthetic-sample-1.0.0.zip')
                page.get_by_role('button', name='上传并预览', exact=True).click()
                page.get_by_role('button', name='确认安装', exact=True).wait_for(timeout=15000)
            upload()
            check('合成ZIP版本出现在预览', '1.0.0' in page.get_by_role('dialog').inner_text())
            page.screenshot(path=str(out / 'zip-preview.png'), full_page=True)
            page.get_by_role('button', name='取消导入', exact=True).click()
            page.get_by_role('dialog').wait_for(state='hidden')
            check('取消未发送安装请求', not any(e['method'] == 'POST' and e['path'].endswith('/install') for e in events))
            check('取消向后端提交删除暂存', any(e['method'] == 'DELETE' and '/imports/' in e['path'] and e['status'] < 300 for e in events))
            upload()
            for label in ['我确认本固定候选兼容性未验证', '我确认启动或重新加载可能联网验证许可']:
                control = page.get_by_label(label, exact=True)
                if control.count():
                    control.check()
            page.get_by_role('button', name='确认安装', exact=True).click()
            page.wait_for_function("document.body.innerText.includes('安装完成')", timeout=20000)
            dialog = page.get_by_role('dialog').inner_text()
            check('插件安装显示实际完成终态', '安装完成' in dialog and '安装失败' not in dialog)
            check('单次确认只发送一次安装', len([e for e in events if e['method'] == 'POST' and e['path'].endswith('/install')]) == 1)
            page.screenshot(path=str(out / 'zip-installed.png'), full_page=True)
            print(json.dumps({'dialog': dialog}, ensure_ascii=False))
        else:
            page.get_by_label('本站发布版本', exact=True).wait_for(timeout=15000)
            page.get_by_label('本站发布版本', exact=True).select_option('v1.2.3')
            page.get_by_role('button', name='生成更新预览', exact=True).click()
            page.get_by_label('确认维护备份影响', exact=True).wait_for(timeout=15000)
            check('维护确认前执行按钮禁用', page.get_by_role('button', name='确认更新此候选', exact=True).is_disabled())
            check('固定候选展示版本与站点', 'v1.2.3' in page.get_by_label('固定更新候选', exact=True).inner_text() and 'fixture' in page.get_by_label('固定更新候选', exact=True).inner_text())
            page.screenshot(path=str(out / 'core-confirmation.png'), full_page=True)
            page.get_by_label('确认维护备份影响', exact=True).check()
            page.get_by_role('button', name='确认更新此候选', exact=True).click()
            page.locator('[data-core-update-state]').wait_for(timeout=15000)
            page.wait_for_function("document.querySelector('[data-core-update-state]')?.innerText.includes('任务编号：')", timeout=15000)
            body = page.locator('[data-core-update-state]').inner_text()
            check('确认后出现真实任务编号', '任务编号：' in body)
            (out / 'core-confirmed.json').write_text(json.dumps({'observed_text': body}, ensure_ascii=False))
            if os.environ.get('RESTART_MANAGER') == '1':
                deadline = time.monotonic() + 20
                while not (out / 'manager-restarted.json').is_file():
                    if time.monotonic() > deadline:
                        raise TimeoutError('缺少独立Manager重启回执')
                    page.wait_for_timeout(100)
                page.reload(wait_until='domcontentloaded')
            page.wait_for_function("document.querySelector('[data-core-update-state]')?.getAttribute('data-core-update-state') === 'success'", timeout=30000)
            result = page.locator('[data-core-update-state]').inner_text()
            check('任务恢复同一编号', body.split('任务编号：',1)[1].split('\n',1)[0].strip() in result)
            check('核心更新显示完成版本', 'v1.2.3' in result)
            check('重连未重复提交更新', len([e for e in events if e['method'] == 'POST' and e['path'].endswith('/install')]) == 1)
            page.screenshot(path=str(out / 'core-completed.png'), full_page=True)
            print(json.dumps({'operation': result}, ensure_ascii=False))
        check('新增v8正式请求未发生404', not any(e['status'] == 404 for e in events))
        print(json.dumps({'checks': checks}, ensure_ascii=False))
    except Exception:
        print(json.dumps({'url': page.url, 'body': page.locator('body').inner_text()[:12000]}, ensure_ascii=False))
        page.screenshot(path=str(out / (stage + '-failure.png')), full_page=True)
        raise
    finally:
        save()
        browser.close()
