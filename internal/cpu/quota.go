// Package cpu answers how much processor this process actually has, which
// is not what runtime.NumCPU reports inside a container.
//
// Go 1.24 derives NumCPU from the scheduling affinity mask. A container
// given a CPU *quota* — two vCPU on Cloud Run, `docker run --cpus=2` —
// keeps the full affinity mask of the node underneath it, so NumCPU
// reports the node's cores and the process cheerfully starts a worker per
// one of them. They then contend for a quota a fraction of that size, and
// everything takes longer than it needs to.
//
// This was measured rather than assumed: the released image under
// `--cpus=2` verified six labels at a mean of 4,299 ms with the pool sized
// from NumCPU and 2,804 ms with it sized from the quota, a third of the
// time given back for reading one file.
package cpu

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Available is the number of cores this process can actually keep busy:
// its cgroup quota where there is one, and NumCPU where there is not. It
// never returns less than one, and never more than NumCPU, since a quota
// larger than the machine is not a promise of anything.
func Available() int {
	n := runtime.NumCPU()
	if q, ok := quota(); ok && q < n {
		n = q
	}
	if n < 1 {
		n = 1
	}
	return n
}

// quota reads the cgroup CPU limit, v2 first and then v1. A limit of a
// fraction of a core rounds up to one: the process still runs, it simply
// runs slowly, and a pool of zero would not run at all.
func quota() (int, bool) {
	// cgroup v2: "$MAX $PERIOD", where $MAX is "max" when unlimited.
	if b, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 && f[0] != "max" {
			return ratio(f[0], f[1])
		}
	}
	// cgroup v1: the two halves in separate files, quota -1 when unlimited.
	max, err1 := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	period, err2 := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if err1 == nil && err2 == nil {
		if q := strings.TrimSpace(string(max)); q != "-1" {
			return ratio(q, strings.TrimSpace(string(period)))
		}
	}
	return 0, false
}

func ratio(maxs, periods string) (int, bool) {
	max, err1 := strconv.Atoi(strings.TrimSpace(maxs))
	period, err2 := strconv.Atoi(strings.TrimSpace(periods))
	if err1 != nil || err2 != nil || period <= 0 || max <= 0 {
		return 0, false
	}
	n := max / period
	if max%period != 0 {
		n++ // half a core is still a core to schedule on
	}
	return n, true
}
