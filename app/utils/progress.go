package utils

import "time"

type ScanProgress struct {
	Fraction                        float64
	Phase                           string
	Path                            string
	Files, Folders, Errors, Skipped int
	Bytes                           int64
	Completed, Total                int
}

func scanReporter(s *Scan, callback func(ScanProgress)) func(string, bool) {
	var last time.Time
	return func(path string, force bool) {
		if callback == nil {
			return
		}
		now := time.Now()
		if !force && now.Sub(last) < 100*time.Millisecond {
			return
		}
		last = now
		callback(ScanProgress{Fraction: s.Fraction, Phase: "Scanning", Path: path, Files: s.Files, Folders: s.Folders, Bytes: s.Bytes, Errors: s.Errors, Skipped: s.Skipped})
	}
}
