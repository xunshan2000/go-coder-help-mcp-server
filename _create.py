import json, subprocess, sys, time
sys.stdout.reconfigure(encoding='utf-8')

def rpc(p, req):
    p.stdin.write(json.dumps(req, ensure_ascii=False)+"\n"); p.stdin.flush()
    return p.stdout.readline()

def call(p, name, args, rid):
    line = rpc(p, {"jsonrpc":"2.0","id":rid,"method":"tools/call",
                   "params":{"name":name,"arguments":args}})
    msg = json.loads(line)
    res = msg.get("result", {})
    text = res.get("content",[{}])[0].get("text","")
    return json.loads(text), res.get("isError", False)

p = subprocess.Popen(["./bin/mcp-server.exe","--config","config.yaml"],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    text=True, encoding="utf-8", bufsize=1)
rpc(p,{"jsonrpc":"2.0","id":1,"method":"initialize",
       "params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"x","version":"0"}}})
p.stdin.write(json.dumps({"jsonrpc":"2.0","method":"notifications/initialized","params":{}})+"\n"); p.stdin.flush()

ZHANGSHI = "32f18c22b0c01e"

# 1) 找 admin 目录
apis, _ = call(p, "apipost_list_apis", {}, 2)
nodes = apis.get("data",{}).get("data",{}).get("list", [])
admin_id = None
for n in nodes:
    if n.get("parent_id") == ZHANGSHI and n.get("target_type") == "folder" and n.get("name","").lower() == "admin":
        admin_id = n.get("target_id"); break
if not admin_id:
    print("找不到 admin 目录，退出"); p.kill(); sys.exit(1)
print(f"admin 目录 target_id={admin_id}")

# 2) 在 admin 下创建 test 子目录
folder_body = {
    "parent_id": admin_id,
    "target_type": "folder",
    "name": "test",
    "description": "由 mcp-server 创建的测试目录；可随时删除",
    "request": {
        "header": {"parameter": []},
        "query": {"parameter": []},
        "body": {"parameter": []},
        "cookie": {"parameter": []},
        "auth": {"type": "inherit"}
    }
}
print("\n创建目录 test...")
res, is_err = call(p, "apipost_create_http_api", {"body": folder_body}, 3)
print(f"  isError={is_err} http_status={res.get('http_status')} msg={res.get('data',{}).get('msg')}")
test_folder_id = (res.get("data",{}).get("data") or {}).get("target_id")
print(f"  test 目录 target_id={test_folder_id}")
if not test_folder_id:
    print(f"  创建失败 raw={json.dumps(res, ensure_ascii=False)[:500]}"); p.kill(); sys.exit(1)

# 3) 在 test 目录下创建 HTTP 接口
ts = time.strftime("%H%M%S")
api_body = {
    "parent_id": test_folder_id,
    "name": f"mcp-test-api-{ts}",
    "method": "GET",
    "url": "/mcp/test/{id}",
    "protocol": "http/1.1",
    "description": "由 mcp-server 创建的测试接口；可随时删除",
    "request": {
        "header": {"parameter": []},
        "query": {"parameter": [
            {"key": "limit", "value": "10", "description": "分页大小", "is_checked": 1, "type": "Integer", "field_type": "Integer", "not_null": 1}
        ]},
        "resful": {"parameter": [
            {"key": "id", "value": "1", "description": "资源 id", "is_checked": 1, "type": "String", "field_type": "String", "not_null": 1}
        ]},
        "body": {"mode": "none", "parameter": [], "raw": "", "raw_para": []},
        "cookie": {"parameter": []},
        "auth": {"type": "inherit"}
    },
    "response": {
        "success": [{
            "name": "成功示例",
            "expect": {"http_code": "200", "name": "成功", "content_type": "json", "verify_type": "schema", "mock": ""},
            "data": {"parameter": [
                {"key": "code", "value": "0", "type": "Integer", "field_type": "Integer", "description": "0=成功", "not_null": 1},
                {"key": "msg", "value": "ok", "type": "String", "field_type": "String", "description": "提示", "not_null": 1}
            ], "raw": "", "raw_schema": {}}
        }]
    }
}
print(f"\n在 test 目录下创建接口 mcp-test-api-{ts}...")
res, is_err = call(p, "apipost_create_http_api", {"body": api_body}, 4)
print(f"  isError={is_err} http_status={res.get('http_status')} msg={res.get('data',{}).get('msg')}")
api_id = (res.get("data",{}).get("data") or {}).get("target_id")
print(f"  接口 target_id={api_id}")

# 4) 列表验证
if api_id:
    print("\n验证 test 目录下能看到此接口:")
    apis2, _ = call(p, "apipost_list_apis", {}, 5)
    for n in apis2.get("data",{}).get("data",{}).get("list", []):
        if n.get("parent_id") == test_folder_id:
            print(f"  - {n.get('target_type'):8s} {n.get('method',''):6s} {n.get('url',''):30s} {n.get('name')}")

p.stdin.close()
try: p.wait(timeout=3)
except: p.kill()
