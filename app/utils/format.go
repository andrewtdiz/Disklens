package utils

import "fmt"

func FormatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v, unit := float64(n), 0
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	for v >= 1024 && unit < len(units)-1 {
		v /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", v, units[unit])
}

func FormatDelta(n *Node) string {
	if !n.Compared {
		return "—"
	}
	if n.Delta > 0 {
		return "+" + FormatSize(n.Delta)
	}
	if n.Delta < 0 {
		return "−" + FormatSize(-n.Delta)
	}
	return "0 B"
}
