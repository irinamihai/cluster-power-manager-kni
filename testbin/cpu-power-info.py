#!/usr/bin/env python3
"""
cpu-power-info.py  -  show governor / epp / freq limits, C-states, uncore freq.

Examples
--------
# all info, all CPUs
./cpu-power-info.py

# only C-state info for CPUs 2-15
./cpu-power-info.py --cpus 2-15 --show cstate

# only pstate & cstate info for CPUs 0 & 1
./cpu-power-info.py -c 0,1 --show pstate,cstate

# only uncore info
./cpu-power-info.py --show uncore
"""
import argparse, os, textwrap, re
from pathlib import Path
from typing import List, Dict, Tuple

SYS_CPU     = Path("/sys/devices/system/cpu")
UNCORE_BASE = Path("/sys/devices/system/cpu/intel_uncore_frequency")
COLS        = ("CPU", "Governor", "EPP", "fMin", "fMax", "C-states", "Uncore")

# ---------- helpers ----------------------------------------------------------
def read_first(path: Path) -> str:
    try:
        return path.read_text().strip()
    except FileNotFoundError:
        return "n/a"

def freq_fmt(khz: str) -> str:
    return f"{int(int(khz)/1000)} MHz" if khz.isdigit() else "n/a"

def cpufreq_paths(cpu: int) -> Tuple[Path, Path, Path, Path]:
    base = SYS_CPU / f"cpu{cpu}" / "cpufreq"
    return (
        base / "scaling_governor",
        base / "energy_performance_preference",
        base / "scaling_min_freq",
        base / "scaling_max_freq",
    )

def list_cstates(cpu: int) -> Dict[str, str]:
    d = SYS_CPU / f"cpu{cpu}" / "cpuidle"
    states = {}
    for s in d.glob("state*"):
        name = read_first(s / "name")
        disabled = read_first(s / "disable")
        states[name] = "enabled" if disabled == "0" else "disabled"
    return states

def gather_uncore() -> Dict[str, str]:
    data = {}
    if not UNCORE_BASE.exists():
        return data
    for pkg in sorted(UNCORE_BASE.iterdir()):
        min_p, max_p = pkg/"min_freq_khz", pkg/"max_freq_khz"
        min_khz, max_khz = read_first(min_p), read_first(max_p)
        if min_khz.isdigit() and max_khz.isdigit():
            tag = pkg.name.replace("package_", "pkg").replace("_die_", "d")
            data[tag] = (freq_fmt(min_khz), freq_fmt(max_khz))
    return data

# ---------- parse CPU list/range --------------------------------------------
def expand_cpu_list(spec: str) -> List[int]:
    parts = spec.split(',')
    cpus: List[int] = []
    rng = re.compile(r'^(\d+)-(\d+)$')
    for p in parts:
        p = p.strip()
        if p.isdigit():
            cpus.append(int(p))
        else:
            m = rng.match(p)
            if not m:
                raise ValueError(f"Bad CPU spec '{p}'")
            start, end = map(int, m.groups())
            if start > end: start, end = end, start
            cpus.extend(range(start, end+1))
    return sorted(set(cpus))

# ---------- gather + print ---------------------------------------------------
def gather_cpu(cpu: int) -> Dict[str, str]:
    gov, epp, fmin, fmax = cpufreq_paths(cpu)
    return {
        "CPU":       f"cpu{cpu}",
        "Governor":  read_first(gov),
        "EPP":       read_first(epp),
        "fMin":      freq_fmt(read_first(fmin)),
        "fMax":      freq_fmt(read_first(fmax)),
        "C-states":  ", ".join(f"{k}:{v}" for k,v in list_cstates(cpu).items())
    }

def print_cpu_table(rows: List[Dict[str,str]], show_p, show_c):
    cols = ["CPU"]
    if show_p:
        cols += ["Governor", "EPP", "fMin", "fMax"]
    if show_c:
        cols += ["C-states"]

    if len(cols) == 1:   # nothing requested at CPU level
        return

    widths = {c: max(len(c), *(len(r[c]) for r in rows)) for c in cols}
    hdr = "  ".join(c.ljust(widths[c]) for c in cols)
    print(hdr)
    print("-"*len(hdr))
    for r in rows:
        print("  ".join(r[c].ljust(widths[c]) for c in cols))
    print()

def print_uncore_table(data: Dict[str, tuple]):
    if not data:
        print("No uncore frequency info found.\n")
        return
    print("Package/Die  Min  Max")
    print("---------------------------")
    for tag, (mn, mx) in data.items():
        print(f"{tag:<11}  {mn:<6} {mx}")
    print()

# ---------- main --------------------------------------------------------------
def main():
    parser  = argparse.ArgumentParser(formatter_class=argparse.RawDescriptionHelpFormatter,
        description=textwrap.dedent(__doc__))
    parser.add_argument("-c","--cpus", help="CPU list/range, e.g. 0,4,8-11 (default = all)")
    parser.add_argument("--show", default="all", help="Limit output, e.g. pstate,cstate,uncore (default = all)")
    args = parser.parse_args()

    show_flags = [s.strip() for s in args.show.split(",")]
    show_p = ("all" in show_flags) or ("pstate" in show_flags)
    show_c = ("all" in show_flags) or ("cstate" in show_flags)
    show_u = ("all" in show_flags) or ("uncore" in show_flags)

    all_cpus = sorted(int(d.name[3:]) for d in SYS_CPU.glob("cpu[0-9]*"))
    cpus = all_cpus if not args.cpus else expand_cpu_list(args.cpus)

    # print tables
    if show_p or show_c:
        cpu_rows = [gather_cpu(c) for c in cpus]
        print_cpu_table(cpu_rows, show_p, show_c)
    if show_u:
        print_uncore_table(gather_uncore())

if __name__ == "__main__":
    main()
