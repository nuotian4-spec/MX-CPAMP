"""在禁网容器内验收真实 CPA 与 Manager 的 SIWC 拒绝授权链路。"""

import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


EVIDENCE = Path("/evidence")
MANAGEMENT_KEY = "synthetic-siwc-management-only"
ADMIN_KEY = "cpamp_SYNTHETIC_SIWC_CROSS_PRODUCT"
CPA = "http://127.0.0.1:8317"
MANAGER = "http://127.0.0.1:18317"
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
RESULT = {
    "scope": "real_cpa_to_real_manager_synthetic_error_callback",
    "observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "network_mode": "none",
    "oauth_browser_opened": False,
    "token_exchange_submitted": False,
    "model_requests": 0,
    "checks": [],
}


def check(name, condition):
    RESULT["checks"].append({"name": name, "passed": bool(condition)})
    if not condition:
        raise AssertionError(name)


def request(base, path, key, method="GET", body=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    headers = {"Authorization": "Bearer " + key}
    if data is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with OPENER.open(req, timeout=3) as response:
            return response.status, json.load(response), dict(response.headers)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error), dict(error.headers)


def ready(base, path, key, processes):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if any(process.poll() is not None for process in processes):
            raise AssertionError("service_exited_before_ready")
        try:
            status, payload, headers = request(base, path, key)
            if status == 200:
                return payload
        except (OSError, ValueError):
            pass
        time.sleep(0.1)
    raise AssertionError("service_readiness_timeout")


def main():
    processes = []
    logs = []
    directory = tempfile.TemporaryDirectory(prefix="siwc-real-chain-", dir="/tmp")
    root = Path(directory.name)
    try:
        auth_dir = root / "auths"
        auth_dir.mkdir(mode=0o700)
        fixture = {
            "type": "codex",
            "email": "synthetic-siwc@example.invalid",
            "account_id": "synthetic-siwc-account",
            "access_token": "SYNTHETIC_ACCESS_NOT_A_REAL_TOKEN",
            "refresh_token": "SYNTHETIC_REFRESH_NOT_A_REAL_TOKEN",
            "expired": "2099-12-31T00:00:00Z",
            "last_refresh": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "disabled": True,
        }
        auth_file = auth_dir / "synthetic-codex.json"
        auth_file.write_text(json.dumps(fixture), encoding="utf-8")
        auth_file.chmod(0o600)
        original_credential = auth_file.read_bytes()
        config = root / "config.yaml"
        config.write_text(
            "config-version: 8\n"
            "server:\n  host: 127.0.0.1\n  port: 8317\n"
            "  server-id: 75641471-8129-4f18-820c-2698fa56d331\n"
            "  kv-store:\n    disabled: true\n"
            "management:\n  allow-remote: false\n"
            f"  secret-key: {MANAGEMENT_KEY}\n"
            "  disable-control-panel: true\n"
            "access:\n  api-keys: []\n"
            f"oauth:\n  auth-dir: {auth_dir}\n"
            "plugins:\n  enabled: false\n",
            encoding="utf-8",
        )
        for source, target in [("/inputs/CLIProxyAPI", root / "cpa"), ("/inputs/manager", root / "manager")]:
            shutil.copyfile(source, target)
            target.chmod(0o700)
        cpa_log = (EVIDENCE / "real-chain-cpa.log").open("w", encoding="utf-8")
        logs.append(cpa_log)
        processes.append(subprocess.Popen([str(root / "cpa"), "-config", str(config), "-local-model"], cwd=root, stdout=cpa_log, stderr=subprocess.STDOUT))
        listed = ready(CPA, "/v8/management/credentials", MANAGEMENT_KEY, processes)
        matching = [entry for entry in listed.get("files", []) if entry.get("name") == auth_file.name]
        check("real_cpa_loads_exact_synthetic_file", len(matching) == 1)
        entry = matching[0]
        identity = {"auth_id": entry["id"], "auth_index": entry["auth_index"]}
        env = dict(os.environ)
        env.update({
            "HTTP_ADDR": "127.0.0.1:18317",
            "USAGE_DATA_DIR": str(root / "manager-data"),
            "CPA_UPSTREAM_URL": CPA,
            "CPA_MANAGEMENT_KEY": MANAGEMENT_KEY,
            "CPA_MANAGER_ADMIN_KEY": ADMIN_KEY,
            "CPAMP_UPDATE_CHECK_ENABLED": "false",
            "USAGE_POLL_INTERVAL_MS": "600000",
            "USAGE_QUOTA_COOLDOWN_ENABLED": "false",
            "USAGE_ACCOUNT_ACTIONS_ENABLED": "false",
            "USAGE_ACCOUNT_ACTIONS_AUTO_DISABLE": "false",
        })
        manager_log = (EVIDENCE / "real-chain-manager.log").open("w", encoding="utf-8")
        logs.append(manager_log)
        processes.append(subprocess.Popen([str(root / "manager")], cwd=root, env=env, stdout=manager_log, stderr=subprocess.STDOUT))
        ready(MANAGER, "/health", ADMIN_KEY, processes)
        status, credential, headers = request(MANAGER, "/api/siwc/credential?" + urllib.parse.urlencode(identity), ADMIN_KEY)
        check("manager_credential_200_and_nostore", status == 200 and headers.get("Cache-Control") == "no-store")
        check("manager_capability_from_real_cpa", credential.get("siwc_bind_supported") is True and credential.get("siwc_bound") is False)
        check("manager_preserves_exact_credential_identity", all(credential.get(key) == value for key, value in identity.items()))
        identity["connection_ref"] = credential["connection_ref"]
        status, binding, _ = request(MANAGER, "/api/siwc/bind", ADMIN_KEY, "POST", identity)
        check("bind_generates_wait_without_token_exchange", status == 200 and binding.get("status") == "wait" and binding.get("flow") == "siwc_bind" and binding.get("expires_in") == 300)
        link = urllib.parse.urlparse(binding["url"])
        query = urllib.parse.parse_qs(link.query)
        check("real_cpa_link_state_and_redirect_match", query.get("state") == [binding["state"]] and query.get("redirect_uri") == ["http://127.0.0.1:1455/auth/callback"])
        session = {**identity, "state": binding["state"]}
        callback = "http://127.0.0.1:1455/auth/callback?" + urllib.parse.urlencode({"state": binding["state"], "error": "access_denied", "error_description": "synthetic rejection"})
        status, submitted, _ = request(MANAGER, "/api/siwc/callback", ADMIN_KEY, "POST", {**session, "redirect_url": callback})
        check("error_only_callback_accepted_without_code_or_client_id", status == 200 and submitted.get("status") == "wait" and submitted.get("submitted") is True)
        status, terminal, _ = request(MANAGER, "/api/siwc/status?" + urllib.parse.urlencode(session), ADMIN_KEY)
        check("real_cpa_error_is_manager_session_failed", status == 200 and terminal.get("status") == "error" and terminal.get("code") == "siwc_session_failed" and "credential" not in terminal)
        check("rejected_authorization_preserves_synthetic_credential", auth_file.read_bytes() == original_credential)
        RESULT["status"] = "passed"
    except Exception as error:
        RESULT["status"] = "failed"
        RESULT["failure"] = str(error) if isinstance(error, AssertionError) else type(error).__name__
    finally:
        for process in reversed(processes):
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=3)
        RESULT["started_services"] = len(processes)
        RESULT["services_stopped"] = all(process.poll() is not None for process in processes)
        for stream in logs:
            stream.close()
        directory.cleanup()
        RESULT["synthetic_state_removed"] = not root.exists()
        (EVIDENCE / "real-chain-result.json").write_text(json.dumps(RESULT, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(json.dumps(RESULT, ensure_ascii=False))
    return 0 if RESULT["status"] == "passed" and RESULT["services_stopped"] and RESULT["synthetic_state_removed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
