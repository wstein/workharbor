import subprocess,sys,time
t=time.perf_counter(); r=subprocess.run(sys.argv[1:],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE); d=time.perf_counter()-
print("%.2f"%d, r.returncode, (r.stderr.decode()[-200:] if r.returncode else ""))
