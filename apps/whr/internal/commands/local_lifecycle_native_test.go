// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalLifecycleDefaultHandlersReachNativeBackend(t *testing.T) {
	handlers := commands.DefaultHandlers()
	for _, id := range []string{"workspace.stage", "workspace.promote"} {
		if handlers[id] == nil {
			t.Errorf("default handler missing for %s", id)
		}
	}
}

// This fixture uses the actual DefaultHandlers, endpoint selection, tool
// discovery, source capture, private receipts and official-CLI routing. The
// installed test scripts model Docker/CLI protocols without running a daemon.
func TestLocalLifecycleDefaultHandlersStageThenPromote(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 fixture interpreter unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "project")
	if err = os.MkdirAll(filepath.Join(source, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{".devcontainer/devcontainer.json": `{"image":"example:local","workspaceFolder":"/workspaces/project","remoteUser":"vscode","postCreateCommand":"sh .devcontainer/setup.sh"}`, ".devcontainer/setup.sh": "printf fixture\n", ".devcontainer/smoke-container.sh": "printf smoke\n", "owner": "owner bytes\n"} {
		if err = os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--template=", "--initial-branch=main"}, {"add", "."}, {"commit", "-m", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("fixture Git: %v %s", e, out)
		}
	}
	listener, err := net.Listen("unix", filepath.Join(root, "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			conn.Close()
		}
	}()
	hostname, _ := os.Hostname()
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	state := map[string]any{"source": source, "hostname": hostname, "kernel": strings.TrimSpace(string(kernel)), "primary": map[string]any{"Id": strings.Repeat("a", 64), "Name": "/primary", "ImageID": "sha256:" + strings.Repeat("b", 64), "ConfigImage": "example:local", "User": "", "Hostname": "primary", "State": "running", "Running": true, "Healthcheck": nil, "Health": nil, "Labels": map[string]string{"devcontainer.local_folder": source, "devcontainer.config_file": filepath.Join(source, ".devcontainer/devcontainer.json")}, "Mounts": []map[string]any{{"Type": "bind", "Source": source, "Destination": "/workspaces/project", "RW": true}, {"Type": "volume", "Name": "anchorage-home-owner", "Source": "/native/home", "Destination": "/home/vscode", "RW": true}}, "Environment": []string{}, "Entrypoint": []string{"/bin/sh"}, "Command": []string{"-c", "fixture"}}, "candidate": nil, "volume": nil, "calls": [][]string{}, "phases": [][]string{}, "stdin": ""}
	statePath := filepath.Join(root, "daemon-state.json")
	data, _ := json.Marshal(state)
	if err = os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(root, "tools")
	if err = os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "fake.py")
	if err = os.WriteFile(helper, []byte(nativeLifecycleFakePython), 0600); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for _, name := range []string{"docker", "node"} {
		script := "#!/bin/sh\nexec " + quote(python) + " " + quote(helper) + " " + name + " " + quote(statePath) + " \"$@\"\n"
		if err = os.WriteFile(filepath.Join(tools, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(tools, "devcontainer"), []byte("#!/usr/bin/env node\n// test-owned CLI entry\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_HOST", "unix://"+listener.Addr().String())
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	invoke := func(args ...string) result {
		var out, diagnostics bytes.Buffer
		code := commands.Execute(context.Background(), args, commands.Env{Stdout: &out, Stderr: &diagnostics, Stdin: strings.NewReader(""), Getenv: os.Getenv}, commands.DefaultHandlers())
		return result{code: code, stdout: out.String(), err: diagnostics.String()}
	}
	preview := invoke("ws", "stage", "--local", "--source", source, "--smoke", "--dry-run", "--json", "primary")
	if preview.code != 0 {
		t.Fatalf("stage preview: %+v", preview)
	}
	envelope := decodeEnvelope(t, preview.stdout)
	if err = compileSchema(t).Validate(envelope); err != nil {
		t.Fatal(err)
	}
	fingerprint := envelope["result"].(map[string]any)["fingerprint"].(string)
	stage := invoke("workspace", "stage", "--local", "--source", source, "--smoke", "--yes", "--trust", fingerprint, "--json", "primary")
	if stage.code != 0 {
		t.Fatalf("stage: %+v", stage)
	}
	envelope = decodeEnvelope(t, stage.stdout)
	if err = compileSchema(t).Validate(envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["result"].(map[string]any)["status"] != "staged_verified" {
		t.Fatal(envelope)
	}
	preview = invoke("workspace", "promote", "--local", "--dry-run", "--json", "primary")
	if preview.code != 0 {
		t.Fatalf("promotion preview: %+v", preview)
	}
	if err = compileSchema(t).Validate(decodeEnvelope(t, preview.stdout)); err != nil {
		t.Fatal(err)
	}
	refused := invoke("workspace", "promote", "--local", "--yes", "--json", "primary")
	requireLifecycleFailure(t, refused, 70, "--volumes and --force")
	promote := invoke("ws", "promote", "--local", "--volumes", "--force", "--yes", "--json", "primary")
	if promote.code != 0 {
		t.Fatalf("promotion: %+v", promote)
	}
	envelope = decodeEnvelope(t, promote.stdout)
	if err = compileSchema(t).Validate(envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["result"].(map[string]any)["status"] != "promoted" {
		t.Fatal(envelope)
	}
	if strings.ContainsRune(stage.err, '\x1b') || strings.ContainsRune(promote.err, '\x1b') || !strings.Contains(promote.err, "<U+001B>[31mfixture-smoke<U+000A>") {
		t.Fatalf("escaped smoke stderr: %q", promote.err)
	}
	data, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	primary := state["primary"].(map[string]any)
	if primary["ImageID"] != "sha256:"+strings.Repeat("d", 64) || state["candidate"] != nil || state["volume"] != nil {
		t.Fatalf("native state: %v", state)
	}
}

const nativeLifecycleFakePython = `import sys,json,os,subprocess,datetime,csv
kind,path=sys.argv[1:3];a=sys.argv[3:];s=json.load(open(path))
def save():
 with open(path,'w') as f: json.dump(s,f)
def emit(v): print(json.dumps(v))
def value(k): return a[a.index(k)+1] if k in a else ''
if kind=='node':
 if a==['--version']: print('v22.0.0');sys.exit()
 if a[-1:] == ['--version']: print('0.89.0');sys.exit()
 phase=a[1];a=a[2:];cfg=json.load(open(value('--override-config')))
 s['phases'].append([phase]+a);save()
 if phase=='read-configuration': emit({'configuration':cfg,'mergedConfiguration':cfg})
 elif phase=='up':
  vector=['run','--mount',cfg['workspaceMount']]
  for m in cfg['mounts']: vector+=['--mount',m]
  vector+=cfg['runArgs']
  for k,v in cfg.get('containerEnv',{}).items():vector+=['-e',k+'='+v]
  if cfg.get('containerUser'):vector+=['--user',cfg['containerUser']]
  for i,v in enumerate(a):
   if v=='--id-label':vector+=['-l',a[i+1]]
  vector+=['--entrypoint','/bin/sh',cfg['image'],'-c','fixture']
  child=subprocess.run([value('--docker-path')]+vector,capture_output=True)
  if child.returncode:sys.stderr.buffer.write(child.stderr);sys.exit(91)
  emit({'outcome':'success','containerId':child.stdout.decode().strip(),'remoteUser':'vscode','remoteWorkspaceFolder':'/workspaces/project','configuration':cfg,'mergedConfiguration':cfg})
 elif phase=='run-user-commands':
  child=subprocess.run([value('--docker-path'),'exec','--user','vscode','--workdir','/workspaces/project',value('--container-id'),'/bin/sh','-c','fixture hooks'],capture_output=True)
  if child.returncode:sys.stderr.buffer.write(child.stderr);sys.exit(92)
  emit({'outcome':'success'})
 else:sys.exit(93)
 sys.exit()
assert kind=='docker' and a[0]=='--host' and a[1].startswith('unix:///');a=a[2:];s['calls'].append(a);save()
if a==['--version']: print('Docker fixture');sys.exit()
pair=':'.join(a[:2])
if pair=='info:--format':emit({'ID':'fixture-daemon','OSType':'linux','OperatingSystem':'Debian','KernelVersion':s['kernel'],'Name':s['hostname'],'Architecture':'x86_64'})
elif pair=='container:ls':
 for c in [s['primary'],s['candidate']]:
  if c:emit({'ID':c['Id'],'Status':c['State']})
elif pair=='container:inspect':
 for id in a[4:]:
  c=next((c for c in [s['primary'],s['candidate']] if c and c['Id']==id),None)
  if c is None:sys.exit(94)
  fmt=value('--format')
  if '{{.Name}} {{index' in fmt:print(c['Name']+' '+c['Labels'].get('workharbor.stage.operation',''))
  elif 'ImageID' in fmt:emit(c)
  else:emit({k:c[v] for k,v in [('Id','Id'),('Name','Name'),('Image','ConfigImage'),('State','State'),('Labels','Labels'),('Mounts','Mounts')]})
elif pair=='image:inspect':emit({'Id':'sha256:'+'d'*64,'Metadata':None})
elif pair=='volume:ls':
 emit('anchorage-home-owner')
 if s['volume']:emit(s['volume']['Name'])
elif pair=='volume:inspect':
 if not s['volume'] or s['volume']['Name']!=a[-1]:sys.exit(95)
 emit(s['volume'])
elif pair=='volume:create':
 labels={}
 for i,v in enumerate(a):
  if v=='--label':k,val=a[i+1].split('=',1);labels[k]=val
 s['volume']={'Name':a[-1],'Driver':'local','CreatedAt':datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z'),'Labels':labels};save();print(a[-1])
elif pair=='container:stop':
 assert s['primary']['Id']==a[-1];s['primary']['Running']=False;s['primary']['State']='exited';save()
elif pair=='container:rm':
 if s['primary'] and s['primary']['Id']==a[-1]:s['primary']=None
 elif s['candidate'] and s['candidate']['Id']==a[-1]:s['candidate']=None
 else:sys.exit(96)
 save()
elif pair=='volume:rm':
 assert s['volume'] and s['volume']['Name']==a[-1] and a[-1]!='anchorage-home-owner';s['volume']=None;save()
elif a[0]=='run':
 mounts=[];labels={};env=[]
 for i,v in enumerate(a):
  if v=='--mount':
   m=dict(x.split('=',1) if '=' in x else (x,'true') for x in next(csv.reader([a[i+1]])));r={'Type':m['type'],'Source':m['source'],'Destination':m['target'],'RW':m.get('readonly')!='true'}
   if m['type']=='volume':r['Name']=m['source']
   mounts.append(r)
  elif v in ['-l','--label']:k,val=a[i+1].split('=',1);labels[k]=val
  elif v in ['-e','--env']:env.append(a[i+1])
 name=value('--name');id=('c' if name=='whr-anchorage-staging' else 'e')*64;image=a[a.index('--entrypoint')+2]
 c={'Id':id,'Name':'/'+name,'ImageID':'sha256:'+'d'*64,'ConfigImage':image,'User':value('--user'),'Hostname':value('--hostname'),'State':'running','Running':True,'Healthcheck':None,'Health':None,'Labels':labels,'Mounts':mounts,'Environment':env,'Entrypoint':['/bin/sh'],'Command':['-c','fixture']}
 if name=='whr-anchorage-staging':s['candidate']=c
 else:
  assert s['primary'] is None;s['primary']=c
 save();print(id)
elif a[0]=='exec':
 if 'bash' in a and '-s' in a:s['stdin']=sys.stdin.read();save();sys.stdout.write('\x1b[31mfixture-smoke\n')
else:sys.exit(97)
`
