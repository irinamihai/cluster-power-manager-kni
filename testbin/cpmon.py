#!/usr/bin/env python3

import glob
import os
import re
import sys
import socket
import json
import curses
import time
import struct
import argparse
import threading

TOPOPATH = "/sys/devices/system/cpu"


def parse_intervals(interval_string):
    intervals = interval_string.split(',')
    result = []

    for interval in intervals:
        if '-' in interval:
            start, end = map(int, interval.split('-'))
            result.extend(range(start, end + 1))
        else:
            result.append(int(interval))

    return result


def connect_to_socket(socket_path):
    try:
        client_socket = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET)
        client_socket.connect(socket_path)
        response = client_socket.recv(16384)
        return client_socket
    except Exception as e:
        return None

def send_command(client_socket, command):
    try:
        client_socket.send(command.encode())
        response = client_socket.recv(16384)
        return json.loads(response.decode())
    except Exception as e:
        return None

def read_usage(socket):
    client_socket = connect_to_socket(socket)
    if not client_socket:
        return None
    command = "/eal/lcore/usage"
    response = send_command(client_socket, command)
    client_socket.close()
    return response

def read_msr(cpu, msr_address):
    try:
        fd = os.open(f"/dev/cpu/{cpu}/msr", os.O_RDONLY)
        os.lseek(fd, msr_address, os.SEEK_SET)
        value = struct.unpack("Q", os.read(fd, 8))[0]
        os.close(fd)
        return value
    except (FileNotFoundError, OSError) as e:
        raise Exception(f"Error accessing MSR: {e}")

def cpu_vendor():
    try:
        with open("/proc/cpuinfo") as f:
            for line in f:
                if line.startswith("vendor_id"):
                    return line.split(":", 1)[1].strip()
    except Exception:
        return ""
    return ""

class Cpu(object):
    def __init__(self, id):
        self.id = id
        self.core_id = -1
        self.cluster_id = -1
        self.die_id = -1
        self.package_id = -1
        self.core = None
        self.cluster = None
        self.die = None
        self.package = None
        self.scaling_governor = None
        self.scaling_setspeed = None
        self.scaling_cur_freq = None
        self.scaling_min_freq = None
        self.scaling_max_freq = None
        self.prev_total_cycles = None
        self.prev_busy_cycles = None
        self.interval_usage = None
        self.energy_counter = None
        self.current_power = None
        self.pkg_energy_counter = None
        self.current_pkg_power = None
        self.last_energy_time = None
        self.last_pkg_energy_time = None
        self.last_tsc = None
        self.last_mperf = None
        self.c0 = None


    def _read_param(self, param):
        param_file = os.path.join(TOPOPATH, "cpu" + str(self.id), param)
        with open(param_file, 'r') as file:
            content = file.read().strip()
        return content

    def _read_param_optional(self, param, default=None):
        param_file = os.path.join(TOPOPATH, "cpu" + str(self.id), param)
        try:
            with open(param_file, 'r') as file:
                content = file.read().strip()
            return content
        except FileNotFoundError:
            return default

    def _read_topology_param(self, param):
        return self._read_param(os.path.join("topology", param))

    def _read_cpufreq_param(self, param):
        return self._read_param(os.path.join("cpufreq", param))

    def read_package(self):
        val = self._read_param_optional(os.path.join("topology", 'physical_package_id'))
        self.package_id = int(val) if val is not None else 0

    def read_die(self):
        # Some platforms (e.g., many ARM SoCs) do not expose die_id
        val = self._read_param_optional(os.path.join("topology", 'die_id'))
        self.die_id = int(val) if val is not None else 0

    def read_cluster(self):
        # cluster_id may be absent; default to 0 which groups by package
        val = self._read_param_optional(os.path.join("topology", 'cluster_id'))
        self.cluster_id = int(val) if val is not None else 0

    def read_core(self):
        val = self._read_param_optional(os.path.join("topology", 'core_id'))
        self.core_id = int(val) if val is not None else self.id

    def read_topology(self):
        self.read_package()
        self.read_die()
        self.read_cluster()
        self.read_core()

    def read_cpufreq(self):
        try:
            self.scaling_governor = self._read_cpufreq_param("scaling_governor")
        except FileNotFoundError:
            return

        if self.scaling_governor == "userspace":
            try:
                self.scaling_setspeed = int(self._read_cpufreq_param("scaling_setspeed"))
            except FileNotFoundError:
                self.scaling_setspeed = None
        else:
            self.scaling_setspeed = 0

        try:
            self.scaling_cur_freq = int(self._read_cpufreq_param("scaling_cur_freq"))
        except FileNotFoundError:
            self.scaling_cur_freq = None
        try:
            self.scaling_min_freq = int(self._read_cpufreq_param("scaling_min_freq"))
        except FileNotFoundError:
            self.scaling_min_freq = None
        try:
            self.scaling_max_freq = int(self._read_cpufreq_param("scaling_max_freq"))
        except FileNotFoundError:
            self.scaling_max_freq = None

    def read_energy(self):
        try:
            vendor = cpu_vendor()
            if vendor == "AuthenticAMD":
                # AMD Fam 17h+ energy MSRs
                energy_unit = read_msr(self.id, 0xC0010299)
                exponent = (energy_unit >> 8) & 0x1F
                # Per-core energy
                core_energy_raw = read_msr(self.id, 0xC001029A)
                # Package energy (best-effort)
                pkg_energy_raw = None
                try:
                    pkg_energy_raw = read_msr(self.id, 0xC001029B)
                except Exception:
                    pkg_energy_raw = None
            elif vendor == "GenuineIntel":
                # Intel RAPL
                energy_unit = read_msr(self.id, 0x606)             # MSR_RAPL_POWER_UNIT
                exponent = (energy_unit >> 8) & 0x1F
                # No true per-core energy on Intel; leave per-core blank
                core_energy_raw = None
                # Package energy
                pkg_energy_raw = read_msr(self.id, 0x611)          # MSR_PKG_ENERGY_STATUS
            else:
                self.current_power = None
                self.current_pkg_power = None
                return

            energy_multiplier = 1 / (2 ** exponent)
            tm = time.time()

            # Compute per-core power (W) where available
            if core_energy_raw is not None:
                energy_core_j = core_energy_raw * energy_multiplier
                if self.energy_counter is not None and self.last_energy_time:
                    self.current_power = (energy_core_j - self.energy_counter) / (tm - self.last_energy_time)
                self.energy_counter = energy_core_j
            else:
                self.current_power = None

            # Compute package power (W) where available
            if pkg_energy_raw is not None:
                energy_pkg_j = pkg_energy_raw * energy_multiplier
                if self.pkg_energy_counter is not None and self.last_pkg_energy_time:
                    self.current_pkg_power = (energy_pkg_j - self.pkg_energy_counter) / (tm - self.last_pkg_energy_time)
                self.pkg_energy_counter = energy_pkg_j
            else:
                self.current_pkg_power = None

            self.last_energy_time = tm
            self.last_pkg_energy_time = tm
        except Exception:
            self.current_power = None
            self.current_pkg_power = None
            return

    def read_c0_residency(self):
        try:
            tsc = read_msr(self.id, 0x10)
            mperf = read_msr(self.id, 0xe7)
        except Exception:
            return

        if self.last_tsc:
            self.c0 = (mperf - self.last_mperf) / (tsc - self.last_tsc)

        self.last_tsc = tsc
        self.last_mperf = mperf

    def get_siblings(self):
        return self.core.cpus

class Core(object):
    def __init__(self, id):
        self.id = id
        self.cpus = {}
        self.cluster = None
        self.die = None
        self.package = None

class Cluster(object):
    def __init__(self, id):
        self.id = id
        self.cpus = {}
        self.cores = {}
        self.die = None
        self.package = None

class Die(object):
    def __init__(self, id):
        self.id = id
        self.cpus = {}
        self.cores = {}
        self.clusters = {}
        self.package = None

class Package(object):
    def __init__(self, id):
        self.id = id
        self.cpus = {}
        self.dies = {}

class CpuTopology(object):
    def __init__(self, pod_uid=None):
        self.cpus = {}
        self.cores = set()
        self.clusters = set()
        self.dies = set()
        self.packages = {}
        self.pod_uid = pod_uid

    def get_or_create_core(self, core_id, cluster_id, die_id, package_id):
        core = next((c for c in self.cores if c.id == core_id and c.cluster.id == cluster_id and c.die.id == die_id and c.package.id == package_id), None)
        if not core:
            core = Core(core_id)
            self.cores.add(core)
        return core

    def get_or_create_cluster(self, cluster_id, die_id, package_id):
        cluster = next((c for c in self.clusters if c.id == cluster_id and c.die.id == die_id and c.package.id == package_id), None)
        if not cluster:
            cluster = Cluster(cluster_id)
            self.clusters.add(cluster)
        return cluster

    def get_or_create_die(self, die_id, package_id):
        die = next((d for d in self.dies if d.id == die_id and d.package.id == package_id), None)
        if not die:
            die = Die(die_id)
            self.dies.add(die)
        return die

    def get_or_create_package(self, package_id):
        package = self.packages.get(package_id)
        if not package:
            package = Package(package_id)
            self.packages[package_id] = package
        return package

    def read_topology(self):
        cpu_dirs = glob.glob(os.path.join(TOPOPATH, "cpu[0-9]*"))
        self.cpus = {(cpu_id := int(re.search(r'\d+$', cpu_dir).group())) : Cpu(cpu_id) for cpu_dir in cpu_dirs}
        for cpu in self.cpus.values():
            cpu.read_topology()

            core = self.get_or_create_core(cpu.core_id, cpu.cluster_id, cpu.die_id, cpu.package_id)
            cluster = self.get_or_create_cluster(cpu.cluster_id, cpu.die_id, cpu.package_id)
            die = self.get_or_create_die(cpu.die_id, cpu.package_id)
            package = self.get_or_create_package(cpu.package_id)

            cpu.core = core
            cpu.cluster = cluster
            cpu.die = die
            cpu.package = package

            core.cpus.setdefault(cpu.id, cpu)
            core.cluster = cluster
            core.die = die
            core.package = package
            
            cluster.cpus.setdefault(cpu.id, cpu)
            cluster.cores.setdefault(core.id, core)
            cluster.die = die
            cluster.package = package

            die.cpus.setdefault(cpu.id, cpu)
            die.cores.setdefault(core.id, core)
            die.clusters.setdefault(cluster.id, cluster)
            die.package = package

            package.cpus.setdefault(cpu.id, cpu)
            package.dies.setdefault(die.id, die)

    def read_usage(self):
        if not self.pod_uid:
            return
        socket_path = os.path.join("/var/lib/power-node-agent/pods", self.pod_uid, "dpdk/rte/dpdk_telemetry.v2")
        
        # Read usage
        dpdk_usage = read_usage(socket_path)
        if not dpdk_usage:
            for cpu in self.cpus.values():
                cpu.interval_usage = None
        else:
            usage_data = dpdk_usage['/eal/lcore/usage']
            lcore_ids = usage_data['lcore_ids']
            total_cycles = usage_data['total_cycles']
            busy_cycles = usage_data['busy_cycles']
            
            # Clear all usage first
            for cpu in self.cpus.values():
                cpu.interval_usage = None
            
            # Map lcore IDs to usage ratios and calculate interval usage
            for i, lcore_id in enumerate(lcore_ids):
                if lcore_id in self.cpus:
                    cpu = self.cpus[lcore_id]
                    
                    # Calculate interval-based usage
                    current_total = total_cycles[i]
                    current_busy = busy_cycles[i]
                    
                    if cpu.prev_total_cycles is not None and cpu.prev_busy_cycles is not None:
                        delta_total = current_total - cpu.prev_total_cycles
                        delta_busy = current_busy - cpu.prev_busy_cycles
                        
                        if delta_total > 0:
                            interval_usage_pct = (delta_busy / delta_total) * 100
                            # Truncate toward zero instead of rounding
                            cpu.interval_usage = str(int(interval_usage_pct))
                        else:
                            cpu.interval_usage = "0"
                    else:
                        # First reading, no interval calculation possible
                        cpu.interval_usage = None
                    
                    # Store current values for next interval
                    cpu.prev_total_cycles = current_total
                    cpu.prev_busy_cycles = current_busy

class CpuParam(object):
    def __init__(self, header, width, fmt, identifier, core_only=False, transform=None):
        self.header = header
        self.width = width
        self.fmt = fmt
        self.identifier = identifier
        self.core_only = core_only
        self.transform = transform

class CpuPresenter(object):
    def __init__(self, scr, params):
        self.scr = scr
        self.params = params

    def print_header(self, row, color_pair):
        column = 0
        max_rows, max_cols = self.scr.getmaxyx()
        for param in self.params:
            fmt = "{:" + param.header[0] + str(param.width) + "}"
            header = fmt.format(param.header[1:])
            text = header
            if param.width >= len(header):
                text += " " * (param.width - len(header) + 1)
            if row < max_rows and column < max_cols:
                self.scr.addnstr(row, column, text, max_cols - column)
            column += param.width + 1
        # column now represents total table width; draw separator limited to table width
        table_width = column
        sep_len = min(table_width, max_cols)
        if row + 1 < max_rows and sep_len > 0:
            self.scr.addnstr(row+1, 0, "=" * sep_len, sep_len)

    def print_cpu(self, row, cpu, color_pair):
        column = 0
        max_rows, max_cols = self.scr.getmaxyx()
        for param in self.params:
            val = getattr(cpu, param.identifier, None)
            try:
                transformed = param.transform(val) if hasattr(param, 'transform') and param.transform else val
                if transformed is None or transformed == "":
                    val_present = ""
                else:
                    val_present = param.fmt.format(val=transformed)
            except Exception:
                val_present = ""
            if row < max_rows and column < max_cols:
                remain = max_cols - column
                self.scr.addnstr(row, column, val_present, remain)
                if param.width >= len(val_present):
                    pad_len = min(remain, param.width - len(val_present) + 1)
                    if pad_len > 0:
                        self.scr.addnstr(row, column + len(val_present), " " * pad_len, pad_len)
            column += param.width + 1

def main(stdscr, args):
    refresh_interval_sec = args.print_interval
    sample_interval_sec = args.sample_interval
    monitor_cpus = parse_intervals(args.cpu)
    monitor_siblings = not args.no_siblings
    monitor_dpdk_telemetry = args.dpdk_pod_uid != ""
    dpdk_pod_uid = args.dpdk_pod_uid
    topology = CpuTopology(dpdk_pod_uid)
    topology.read_topology()

    cpu_presenter = CpuPresenter(stdscr, [
        CpuParam("<CPU", 6, "cpu{val:<3}", "id"),
        # Frequencies in sysfs are in kHz; show MHz
        CpuParam(">fCur", 5, "{val:>5}", "scaling_cur_freq", transform=lambda v: (int(v)//1000) if v is not None else ""),
        CpuParam(">Governor", 12, "{val:>12}", "scaling_governor"),
        CpuParam(">fTgt", 5, "{val:>5}", "scaling_setspeed", transform=lambda v: (int(v)//1000) if v not in (None, 0) else ""),
        CpuParam(">fMin", 5, "{val:>5}", "scaling_min_freq", transform=lambda v: (int(v)//1000) if v is not None else ""),
        CpuParam(">fMax", 5, "{val:>5}", "scaling_max_freq", transform=lambda v: (int(v)//1000) if v is not None else ""),
        CpuParam(">IUsg%", 4, "{val:>4}", "interval_usage"),
        CpuParam(">C0%", 4, "{val:>4.0f}", "c0", transform=lambda v: (v*100) if v is not None else ""),
        # Per-core power in mW
        CpuParam(">CoremW", 6, "{val:>6}", "current_power", core_only=True, transform=lambda v: ("{:.0f}".format(v*1000)) if v is not None else ""),
        # Package power in W
        CpuParam(">PkgW", 6, "{val:>6.1f}", "current_pkg_power", core_only=True, transform=lambda v: v if v is not None else ""),
        ])

    stdscr.clear()
    # Enable scrolling if requested
    if getattr(args, "scroll", False):
        stdscr.scrollok(True)
        # In scroll mode, we print a header per snapshot; start at top
        next_row = 0
    else:
        cpu_presenter.print_header(0, 1)
        stdscr.refresh()
        # Track next row for overwrite mode (after header + separator)
        next_row = 2

    # Start a background sampler to read DPDK usage at sample_interval_sec
    sampler_started = False
    stop_event = threading.Event()
    if monitor_dpdk_telemetry:
        def sampler():
            while not stop_event.is_set():
                try:
                    topology.read_usage()
                except Exception:
                    pass
                time.sleep(sample_interval_sec)

        t = threading.Thread(target=sampler, daemon=True)
        t.start()
        sampler_started = True

    while True:
        # If sampler is not running (e.g., dpdk disabled), do a single read per refresh
        if monitor_dpdk_telemetry and not sampler_started:
            topology.read_usage()
        # In append mode, pre-scroll to ensure space for the next snapshot
        if getattr(args, "scroll", False):
            max_rows, _ = stdscr.getmaxyx()
            lines_needed = 0
            for cpu_id in monitor_cpus:
                lines_needed += 1
                if monitor_siblings:
                    lines_needed += max(0, len(topology.cpus[cpu_id].get_siblings()) - 1)
            lines_needed += 3  # header + separator + blank line
            if next_row + lines_needed >= max_rows:
                scroll_by = next_row + lines_needed - (max_rows - 1)
                if scroll_by < 1:
                    scroll_by = 1
                stdscr.scroll(scroll_by)
                next_row = max(2, next_row - scroll_by)
        # Start row for this snapshot
        row = next_row if getattr(args, "scroll", False) else 2
        # Print header for each snapshot in scroll mode
        if getattr(args, "scroll", False):
            cpu_presenter.print_header(row, 1)
            row += 2

        for cpu_id in monitor_cpus:
            cpu = topology.cpus[cpu_id]
            cpu.read_cpufreq()
            cpu.read_energy()
            cpu.read_c0_residency()
            cpu_presenter.print_cpu(row, cpu, 3)
            row += 1
            if monitor_siblings:
                for sibling in cpu.get_siblings().values():
                    if sibling.id != cpu.id:
                        sibling.read_cpufreq()
                        sibling.read_c0_residency()
                        cpu_presenter.print_cpu(row, sibling, 2)
                        row += 1
        # Leave a blank line between snapshots in append mode and advance next_row
        if getattr(args, "scroll", False):
            next_row = row + 1

        stdscr.refresh()
        time.sleep(refresh_interval_sec)

if __name__=="__main__":
    parser = argparse.ArgumentParser(description="Cluster Power Manager CPU monitor")
    parser.add_argument("--print-interval", "-pi", type=float, default=1.0, help="refresh interval in seconds")
    parser.add_argument("--sample-interval", "-si", type=float, default=0.01, help="sampling interval for DPDK usage (seconds)")
    parser.add_argument("--cpu", "-c", type=str, default="0", help="List of cpus to watch")
    parser.add_argument("--no-siblings", "-s", action="store_true", help="Don't watch siblings")
    parser.add_argument("--dpdk-pod-uid", "-d", type=str, default="", help="Watch dpdk usage using dpdk application pod")
    parser.add_argument("--scroll", action="store_true",
                    help="append snapshots instead of refreshing in-place")
    args = parser.parse_args()
    curses.wrapper(main, args)
