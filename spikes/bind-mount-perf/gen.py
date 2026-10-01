#!/usr/bin/env python3
"""Generate the two workloads of spike #40 on the host and commit them.

nodemods: a node_modules-style tree, tracked in git (the worst case for `git status`).
large:    a large repository: many small files plus C sources the build compiles.
"""
import os, subprocess, sys

def tree(root, dirs, per_dir, size, ext, c_sources=0):
    for d in range(dirs):
        p = os.path.join(root, f"pkg{d // 50}", f"mod{d}", "lib")
        os.makedirs(p, exist_ok=True)
        for f in range(per_dir):
            with open(os.path.join(p, f"f{f}.{ext}"), "w") as fh:
                fh.write((f"// {d} {f}\n" + "x" * 60 + "\n") * (size // 64))
    if c_sources:
        os.makedirs(os.path.join(root, "src"), exist_ok=True)
        for i in range(c_sources):
            with open(os.path.join(root, "src", f"u{i}.c"), "w") as fh:
                fh.write(f"int f{i}(int a){{int s=0;for(int i=0;i<a;i++)s+=i*{i%7+1};return s;}}\n")

def git(root, *a):
    subprocess.run(["git", "-C", root, "-c", "user.name=s", "-c", "user.email=s@s", *a], check=True, stdout=subprocess.DEVNULL)

out, which = sys.argv[1], sys.argv[2]
root = os.path.join(out, which, "repo")
os.makedirs(root)
if which == "nodemods":
    tree(root, dirs=4000, per_dir=10, size=2048, ext="js")          # 40 000 files, ~80 MB
else:
    tree(root, dirs=3000, per_dir=50, size=1024, ext="txt", c_sources=3000)  # 153 000 files, ~150 MB
git(root, "init", "-q")
git(root, "add", "-A")
git(root, "commit", "-q", "-m", "seed")
