# Spike #39: memory at 1 and 4 containers (incomplete)

`run.sh [mem] [hold-mib]` starts 1 and then 4 containers with `-m` memory, idle and then each holding `hold-mib` MiB, and records the host's free-memory percentage, page-outs, swap and the resident size of the container VM processes.

`results-2G-1000.txt` is one run on a 16 GiB development laptop (macOS) that was already using about 17 of 18 GiB of swap, with other work running. It does not answer the issue: the resident size of the VM processes does not track what the guests hold (56 MiB for one container holding 1000 MiB, 1114 MiB for four), because the host compressed or swapped it, and the free-memory percentage moves by more than the containers cost. The measurement has to run on the idle Mac mini (#73).
