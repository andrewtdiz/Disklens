package tests

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type client struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func TestDesktopBatchedDirectory(t *testing.T) {
	exe := os.Getenv("DISKLENS_EXE")
	if exe == "" || runtime.GOOS != "windows" {
		t.Skip("Requires Windows and DISKLENS_EXE")
	}
	base := t.TempDir()
	root := filepath.Join(base, "files")
	for i := 0; i < 700; i++ {
		name := fmt.Sprintf("file-%04d.dat", i)
		if i == 699 {
			name = "日本語-🙂.dat"
		}
		put(t, root, filepath.Join("Batch", name), 4096)
	}
	if err := os.Link(filepath.Join(root, "Batch", "file-0000.dat"), filepath.Join(root, "Batch", "z-hardlink.dat")); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join("Long-directory-name-123456789012345678901234567890", "nested-directory-name-123456789012345678901234567890", "nested-directory-name-123456789012345678901234567890", "nested-directory-name-123456789012345678901234567890")
	put(t, root, filepath.Join(deep, "long-path.dat"), 4096)
	junction := filepath.Join(root, "loop")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, root).CombinedOutput(); err != nil {
		t.Fatalf("junction: %v %s", err, output)
	}
	defer os.Remove(junction)
	data := filepath.Join(base, "state")
	c, _ := launch(t, exe, data, filepath.Join(base, "batch.sock"))
	c.value("path", root)
	c.press("refresh")
	c.waitFor("No baseline - scan saved - depth 5")
	c.waitFor("702 files")
	c.waitFor("1 links / special entries skipped")
	paths, _ := filepath.Glob(filepath.Join(data, "snapshots", "*.json"))
	if len(paths) != 1 {
		t.Fatal("snapshot missing")
	}
	var snap struct {
		Directories map[string]struct{ Size int64 }
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(savedLatest(t, b), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Directories["."].Size != 701*4096 || snap.Directories["Batch"].Size != 700*4096 {
		t.Fatalf("allocation or hard-link totals differ: %s", b)
	}
	if snap.Directories[deep].Size != 4096 {
		t.Fatal("long path was not scanned")
	}
	t.Log("Multi-batch Windows enumeration, Unicode, long paths, hard-link deduplication and junction skipping passed through the native app")
}

func (c *client) call(method string, params any) json.RawMessage {
	c.t.Helper()
	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	b, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
	if _, err := fmt.Fprintln(c.conn, string(b)); err != nil {
		c.t.Fatal(err)
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		c.t.Fatal(err)
	}
	var r struct {
		Result json.RawMessage
		Error  any
	}
	if err = json.Unmarshal(line, &r); err != nil {
		c.t.Fatal(err)
	}
	if r.Error != nil {
		c.t.Fatalf("%s: %v", method, r.Error)
	}
	return r.Result
}

func (c *client) press(key string) { c.call("act", map[string]any{"key": key, "action": "press"}) }
func (c *client) value(key, text string) {
	c.call("act", map[string]any{"key": key, "action": "setValue", "text": text})
}

type node struct {
	ID, Key, Role, Label, Value, Parent string
	States                              []string
	Bounds                              struct{ X, Y, Width, Height float64 }
}

func (c *client) nodes() []node {
	var r struct{ Nodes []node }
	json.Unmarshal(c.call("tree", nil), &r)
	return r.Nodes
}

func (c *client) waitFor(text string) []node {
	c.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var nodes []node
	for time.Now().Before(deadline) {
		nodes = c.nodes()
		for _, n := range nodes {
			if strings.Contains(n.Label, text) {
				return nodes
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.t.Fatalf("UI never showed %q: %+v", text, nodes)
	return nil
}

func put(t *testing.T, root, path string, size int) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func launch(t *testing.T, exe, data, sock string) (*client, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data, "MYGO_AUTOMATION="+sock)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			t.Cleanup(func() { conn.Close() })
			c := &client{t, conn, bufio.NewReader(conn)}
			for time.Now().Before(deadline) {
				var windows struct{ Windows []any }
				json.Unmarshal(c.call("windows", nil), &windows)
				if len(windows.Windows) > 0 {
					c.waitFor("Folder path")
					return c, cmd
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatal("Application socket opened but no window appeared")
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Application did not open automation socket")
	return nil, nil
}

func TestDesktopScanAndDiff(t *testing.T) {
	exe := os.Getenv("DISKLENS_EXE")
	if exe == "" {
		t.Skip("Set DISKLENS_EXE to an app built with -tags e2e")
	}
	base := t.TempDir()
	root := filepath.Join(base, "files")
	data := filepath.Join(base, "state")
	put(t, root, "Growth/a.bin", 65536)
	put(t, root, "Shrink/b.bin", 262144)
	put(t, root, "Removed/c.bin", 98304)
	put(t, root, "Deep/one/two/three/four/five/six.bin", 32768)
	c, process := launch(t, exe, data, filepath.Join(base, "app.sock"))
	c.value("path", root)
	c.press("refresh")
	c.waitFor("No baseline - scan saved - depth 5")
	paths, err := filepath.Glob(filepath.Join(data, "snapshots", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("snapshots: %v %v", paths, err)
	}
	var snap struct {
		Depth       int
		Directories map[string]struct{ Size int64 }
	}
	b, _ := os.ReadFile(paths[0])
	if err = json.Unmarshal(savedLatest(t, b), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Depth != 5 || snap.Directories["Growth"].Size != 65536 {
		t.Fatalf("wrong snapshot: %s", b)
	}
	if _, ok := snap.Directories[filepath.Join("Deep", "one", "two", "three", "four", "five")]; ok {
		t.Fatal("snapshot exceeded depth 5")
	}
	if snap.Directories[filepath.Join("Deep", "one", "two", "three", "four")].Size != 32768 {
		t.Fatal("deep file was not aggregated")
	}
	assertCLIError(t, data, "NO_BASELINE", root)
	c.press("set-baseline")
	c.waitFor("Baseline set")
	initial := readCLIReport(t, data, root)
	if len(initial.Changes) != 0 {
		t.Fatal("new baseline has a diff")
	}
	put(t, root, "Growth/a.bin", 131072)
	put(t, root, "Shrink/b.bin", 65536)
	if err = os.Remove(filepath.Join(root, "Removed", "c.bin")); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, "Removed")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "Added/new.bin", 32768)
	c.press("refresh")
	c.waitFor("Compared with")
	report := readCLIReport(t, data, root)
	if len(report.Changes) != 5 {
		t.Fatalf("unchanged directories leaked into diff: %+v", report)
	}
	if report.BaselineAt != initial.BaselineAt {
		t.Fatal("refresh moved the baseline")
	}
	assertChange(t, report, filepath.Join(root, "Growth"), "modified", 65536, 131072)
	assertChange(t, report, filepath.Join(root, "Shrink"), "modified", 262144, 65536)
	assertChange(t, report, filepath.Join(root, "Removed"), "removed", 98304, 0)
	assertChange(t, report, filepath.Join(root, "Added"), "added", 0, 32768)
	beforeRead, _ := os.ReadFile(paths[0])
	put(t, root, "unscanned.tmp", 4096)
	unchanged := readCLIReport(t, data, root)
	afterRead, _ := os.ReadFile(paths[0])
	if !reflect.DeepEqual(report, unchanged) || string(beforeRead) != string(afterRead) {
		t.Fatal("CLI scanned or changed saved state")
	}
	if err := os.Remove(filepath.Join(root, "unscanned.tmp")); err != nil {
		t.Fatal(err)
	}
	nodes := c.waitFor("Removed  (removed)")
	for _, n := range nodes {
		if n.Role == "treeItem" && (strings.Contains(n.Label, "%") || n.Bounds.Height > 24) {
			t.Fatalf("tree row is not compact or contains a percentage: %+v", n)
		}
		if strings.Contains(n.Label, "+ growth") || strings.Contains(n.Label, "− freed") || strings.Contains(n.Label, "Scroll down:") {
			t.Fatalf("redundant helper text remains: %s", n.Label)
		}
	}
	for name, delta := range map[string]string{"Growth": "+64.00 KiB", "Shrink": "−192.00 KiB", "Removed": "−96.00 KiB", "Added": "+32.00 KiB"} {
		found := false
		for _, n := range nodes {
			if n.Role == "treeItem" && strings.HasPrefix(n.Label, name) && strings.Contains(n.Label, delta) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s %s: %+v", name, delta, nodes)
		}
	}
	for _, n := range nodes {
		if n.Role == "treeItem" && strings.HasPrefix(n.Label, "Growth ") {
			for _, wantOpen := range []bool{true, false, true} {
				c.call("input", map[string]any{"events": []any{map[string]any{"type": "click", "x": n.Bounds.X + 150, "y": n.Bounds.Y + n.Bounds.Height/2, "clicks": 2}}})
				c.call("idle", map[string]any{"timeout": 3000})
				found := false
				for _, child := range c.nodes() {
					if child.Role == "treeItem" && strings.HasPrefix(child.Label, "a.bin ") {
						found = true
					}
				}
				if found != wantOpen {
					t.Fatalf("double-click folder: child visible=%v, want %v", found, wantOpen)
				}
			}
			break
		}
	}
	c.waitFor("a.bin")
	for _, n := range c.nodes() {
		if n.Key == "treemap" {
			c.call("input", map[string]any{"events": []any{map[string]any{"type": "click", "x": n.Bounds.X + 40, "y": n.Bounds.Y + 12, "clicks": 2}}})
			break
		}
	}
	c.call("idle", map[string]any{"timeout": 3000})
	for _, n := range c.nodes() {
		if n.Key == "treemap" {
			for _, step := range []struct {
				dy   float64
				want string
			}{{-120, "Growth "}, {-120, root}, {120, "Growth "}} {
				c.call("input", map[string]any{"events": []any{map[string]any{"type": "wheel", "x": n.Bounds.X + 40, "y": n.Bounds.Y + 40, "dy": step.dy, "dx": 0, "precise": false}}})
				c.call("idle", map[string]any{"timeout": 3000})
				matched := false
				for _, row := range c.nodes() {
					if row.Role == "treeItem" && strings.HasPrefix(row.Label, step.want) {
						for _, state := range row.States {
							if state == "selected" || state == "checked" {
								matched = true
							}
						}
					}
				}
				if !matched {
					t.Fatalf("wheel did not select %q: %+v", step.want, c.nodes())
				}
			}
			break
		}
	}
	nodes = c.nodes()
	for _, n := range nodes {
		if n.Role == "textField" && n.Value == "5" {
			c.call("act", map[string]any{"id": n.ID, "action": "setValue", "text": "2"})
			break
		}
	}
	c.press("refresh")
	c.waitFor("Compared with")
	c.waitFor("depth 2")
	repeated := readCLIReport(t, data, root)
	if repeated.BaselineAt != initial.BaselineAt {
		t.Fatal("repeated refresh moved baseline")
	}
	assertChange(t, repeated, filepath.Join(root, "Growth"), "modified", 65536, 131072)
	for _, change := range repeated.Changes {
		if strings.Contains(change.Path, filepath.Join("Deep", "one", "two")) {
			t.Fatal("diff exceeds common snapshot depth")
		}
	}
	c.press("set-baseline")
	c.waitFor("Baseline set")
	if len(readCLIReport(t, data, root).Changes) != 0 {
		t.Fatal("reset did not clear diff")
	}
	for _, node := range c.nodes() {
		if strings.Contains(node.Label, "(removed)") {
			t.Fatal("reset retained a removed directory")
		}
	}
	b, _ = os.ReadFile(paths[0])
	json.Unmarshal(savedLatest(t, b), &snap)
	if snap.Depth != 2 {
		t.Fatal("depth edit was not saved")
	}
	var capture struct{ PNG string }
	json.Unmarshal(c.call("capture", nil), &capture)
	if len(capture.PNG) < 1000 {
		t.Fatal("empty native capture")
	}
	if path := os.Getenv("DISKLENS_CAPTURE"); path != "" {
		image, _ := base64.StdEncoding.DecodeString(capture.PNG)
		if err = os.WriteFile(path, image, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c.value("path", filepath.Join(base, "missing"))
	c.press("refresh")
	if runtime.GOOS == "windows" {
		c.waitFor("cannot find")
	} else {
		c.waitFor("no such file or directory")
	}
	if len(readCLIReport(t, data, root).Changes) != 0 {
		t.Fatal("failed scan changed saved comparison")
	}
	put(t, root, "Growth/a.bin", 196608)
	_ = process.Process.Kill()
	_ = process.Wait()
	c.conn.Close()
	c, _ = launch(t, exe, data, filepath.Join(base, "restart.sock"))
	c.value("path", root)
	c.press("refresh")
	c.waitFor("Compared with")
	nodes = c.waitFor("+64.00 KiB")
	found := false
	for _, n := range nodes {
		if strings.Contains(n.Label, "depth 2") {
			found = true
		}
	}
	if !found {
		t.Fatal("snapshot depth lost after restart")
	}
	t.Log("Native desktop scan, allocation totals, deep aggregation, added/deleted/grown/shrunk folders, tree expansion, treemap navigation, depth edit, missing path, restart persistence, and rendering passed")
}
