import json, subprocess, sys
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

# 1) 默认项目详情
proj, _ = call(p, "apipost_get_project", {}, 2)
proj_data = proj.get("data",{}).get("data",{})
print(f"默认项目: name={proj_data.get('name')!r} project_id={proj_data.get('project_id')!r} project_code={proj_data.get('project_code')!r}")

# 2) 列出接口树看顶层目录
apis, _ = call(p, "apipost_list_apis", {}, 3)
nodes = apis.get("data",{}).get("data",{}).get("list", [])
print(f"\n顶层节点 (parent_id=='0'):")
for n in nodes:
    if n.get("parent_id") == "0":
        print(f"  - {n.get('target_type'):8s} target_id={n.get('target_id'):20s} name={n.get('name')!r}")

p.stdin.close()
try: p.wait(timeout=3)
except: p.kill()
