import axios, { type InternalAxiosRequestConfig } from "axios";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createCoreUpdateApi, createPluginImportApi } from "./pluginImports";

const originalAdapter = axios.defaults.adapter;
const originalBase = axios.defaults.baseURL;
const originalAuthorization = axios.defaults.headers.common.Authorization;
let requests: InternalAxiosRequestConfig[];
let handshakes: InternalAxiosRequestConfig[];
let response: unknown;
let connectionRef: string | undefined;
let rejectBusinessRequest: boolean;

beforeEach(() => {
  requests = [];
  handshakes = [];
  response = {};
  connectionRef = "a".repeat(32);
  rejectBusinessRequest = false;
  axios.defaults.adapter = async (config) => {
    if (config.headers.get("X-CPA-Connection-Ref") === "required") {
      handshakes.push(config);
      return {
        config,
        data: { operation: null },
        status: 200,
        statusText: "OK",
        headers: connectionRef ? { "x-cpa-connection-ref": connectionRef } : {},
      };
    }
    requests.push(config);
    if (rejectBusinessRequest) {
      throw { response: { status: 409 }, message: "CPA connection changed" };
    }
    return {
      config,
      data: response,
      status: 200,
      statusText: "OK",
      headers: {},
    };
  };
});
afterEach(() => {
  axios.defaults.adapter = originalAdapter;
  axios.defaults.baseURL = originalBase;
  if (originalAuthorization === undefined)
    delete axios.defaults.headers.common.Authorization;
  else axios.defaults.headers.common.Authorization = originalAuthorization;
});

describe("固定 CPA 更新 API 契约", () => {
  it("真实 Axios 转换后仍发送原始 ZIP，不包 FormData 或 JSON", async () => {
    const api = createPluginImportApi(
      "https://cpa-one.test/v0/management/",
      "test-key-one",
    );
    const file = new File(["synthetic zip"], "plugin.zip", {
      type: "application/zip",
    });
    response = { id: "preview-one" };
    await expect(api.upload(file)).resolves.toEqual(response);
    expect(requests).toHaveLength(1);
    expect(requests[0].url).toBe(
      "https://cpa-one.test/v8/management/plugins/imports",
    );
    expect(requests[0].method).toBe("post");
    expect(requests[0].data).toBe(file);
    expect(requests[0].headers.get("Content-Type")).toBe("application/zip");
    expect(requests[0].headers.get("Authorization")).toBe(
      "Bearer test-key-one",
    );
    expect(requests[0].timeout).toBe(120_000);
  });

  it("插件确认、取消与任务查询符合固定路由和双确认契约", async () => {
    const api = createPluginImportApi("https://cpa-one.test", "test-key-one");
    await api.install("candidate/one", {
      confirm_unverified: true,
      confirm_license_network: true,
    });
    await api.cancel("candidate/one");
    await api.getOperation("operation/one");
    response = { operation: null };
    await expect(api.getLatestOperation()).resolves.toEqual({
      operation: null,
    });
    expect(requests.map(({ method, url }) => [method, url])).toEqual([
      [
        "post",
        "https://cpa-one.test/v8/management/plugins/imports/candidate%2Fone/install",
      ],
      [
        "delete",
        "https://cpa-one.test/v8/management/plugins/imports/candidate%2Fone",
      ],
      [
        "get",
        "https://cpa-one.test/v8/management/updates/operations/operation%2Fone",
      ],
      [
        "get",
        "https://cpa-one.test/v8/management/updates/operations/latest?type=plugin",
      ],
    ]);
    expect(JSON.parse(requests[0].data)).toEqual({
      confirm: true,
      confirm_unverified: true,
      confirm_license_network: true,
    });
    expect(requests[0].headers.get("Content-Type")).toBe("application/json");
  });

  it("核心发现、候选生成与安装仅走 CPA v8 路由", async () => {
    const api = createCoreUpdateApi(
      "https://cpa-one.test/v8/management",
      "test-key-one",
    );
    await api.discover();
    await api.preview("site-release-two");
    await api.install("fixed/candidate");
    await api.getOperation("core-operation");
    expect(requests.map(({ method, url }) => [method, url])).toEqual([
      ["get", "https://cpa-one.test/v8/management/updates/core"],
      ["post", "https://cpa-one.test/v8/management/updates/core/previews"],
      [
        "post",
        "https://cpa-one.test/v8/management/updates/core/previews/fixed%2Fcandidate/install",
      ],
      [
        "get",
        "https://cpa-one.test/v8/management/updates/operations/core-operation",
      ],
    ]);
    expect(JSON.parse(requests[1].data)).toEqual({
      release_id: "site-release-two",
    });
    expect(JSON.parse(requests[2].data)).toEqual({ confirm: true });
    expect(requests.every((request) => request.timeout === 45_000)).toBe(true);
  });

  it("创建另一连接和改变全局 Axios 默认值不改变已捕获的 CPA 与密钥", async () => {
    const first = createPluginImportApi("https://cpa-one.test", "test-key-one");
    const second = createCoreUpdateApi("https://cpa-two.test", "test-key-two");
    axios.defaults.baseURL = "https://manager.test/usage-service";
    axios.defaults.headers.common.Authorization = "Bearer global-test-key";
    await second.discover();
    await first.getOperation("original-operation");
    expect(
      requests.map((request) => [
        request.url,
        request.headers.get("Authorization"),
      ]),
    ).toEqual([
      [
        "https://cpa-two.test/v8/management/updates/core",
        "Bearer test-key-two",
      ],
      [
        "https://cpa-one.test/v8/management/updates/operations/original-operation",
        "Bearer test-key-one",
      ],
    ]);
  });

  it("并发首次请求只握手一次，固定同一 Manager 连接引用", async () => {
    const api = createCoreUpdateApi("https://manager.test", "admin-key");
    await Promise.all([api.discover(), api.getOperation("original-operation")]);
    expect(handshakes).toHaveLength(1);
    expect(handshakes[0].method).toBe("get");
    expect(handshakes[0].url).toBe(
      "https://manager.test/v8/management/updates/operations/latest?type=plugin",
    );
    expect(requests).toHaveLength(2);
    expect(
      requests.every(
        (request) =>
          request.headers.get("X-CPA-Connection-Ref") === connectionRef,
      ),
    ).toBe(true);
  });

  it("Manager 换靶 409 后保留原引用，不自动重绑或重放安装", async () => {
    const api = createCoreUpdateApi("https://manager.test", "admin-key");
    await api.preview("v1.2.3");
    connectionRef = "b".repeat(32);
    rejectBusinessRequest = true;
    await expect(api.install("fixed-candidate")).rejects.toMatchObject({
      response: { status: 409 },
    });
    await expect(api.getOperation("fixed-operation")).rejects.toMatchObject({
      response: { status: 409 },
    });
    expect(handshakes).toHaveLength(1);
    expect(requests).toHaveLength(3);
    expect(
      requests.every(
        (request) =>
          request.headers.get("X-CPA-Connection-Ref") === "a".repeat(32),
      ),
    ).toBe(true);
    expect(
      requests.filter((request) => request.url?.endsWith("/install")),
    ).toHaveLength(1);
  });

  it("直连 CPA 无 Manager 引用时保留原始 ZIP 和原有安装请求", async () => {
    connectionRef = undefined;
    const api = createPluginImportApi("https://direct-cpa.test", "cpa-key");
    const file = new File(["synthetic zip"], "plugin.zip", {
      type: "application/zip",
    });
    await api.upload(file);
    await api.install("direct-import", {
      confirm_unverified: true,
      confirm_license_network: false,
    });
    expect(handshakes).toHaveLength(1);
    expect(requests).toHaveLength(2);
    expect(requests[0].data).toBe(file);
    expect(
      requests.every((request) => !request.headers.has("X-CPA-Connection-Ref")),
    ).toBe(true);
    expect(
      requests.every(
        (request) => request.headers.get("Authorization") === "Bearer cpa-key",
      ),
    ).toBe(true);
  });

  it.each([
    ["", "test-key-one"],
    ["https://cpa-one.test", ""],
  ])("缺失连接字段时停止发请求：%s", async (base, key) => {
    await expect(createCoreUpdateApi(base, key).discover()).rejects.toThrow(
      "请先连接目标 CPA 管理服务",
    );
    expect(requests).toHaveLength(0);
  });
});
