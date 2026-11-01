//go:build !cli

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type deletionBatch struct {
	directory string
	names     []string
}

type deletionPlan struct {
	batches     []deletionBatch
	directories []string
	total       int
}

func (p *deletionPlan) add(directory string, names []string) {
	p.directories = append(p.directories, directory)
	p.total += len(names) + 1
	for len(names) > 0 {
		count := min(len(names), 64)
		p.batches = append(p.batches, deletionBatch{directory, names[:count]})
		names = names[count:]
	}
}

func retainedDeletionPlan(node *Node) deletionPlan {
	var plan deletionPlan
	var visit func(*Node, string)
	visit = func(n *Node, relative string) {
		var names []string
		for _, child := range n.Children() {
			if !child.Removed && !child.Dir {
				names = append(names, child.Name)
			}
		}
		plan.add(relative, names)
		for _, child := range n.Children() {
			if !child.Removed && child.Dir {
				visit(child, filepath.Join(relative, child.Name))
			}
		}
	}
	visit(node, ".")
	return plan
}

func remainingDeletionPlan(ctx context.Context, root *os.Root) (deletionPlan, error) {
	var plan deletionPlan
	var visit func(string) error
	visit = func(relative string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := root.Open(relative)
		if err != nil {
			return err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return err
		}
		var names, directories []string
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() && !skipEntry(info) {
				directories = append(directories, entry.Name())
			} else {
				names = append(names, entry.Name())
			}
		}
		plan.add(relative, names)
		for _, name := range directories {
			if err := visit(filepath.Join(relative, name)); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(".")
	return plan, err
}

func (d *folderDeletion) validate() error {
	info, err := d.root.Lstat(d.relative)
	if err != nil {
		return err
	}
	if !os.SameFile(d.info, info) || !info.IsDir() || skipEntry(info) {
		return errors.New("The folder changed after confirmation; deletion stopped")
	}
	return nil
}

func (d *folderDeletion) remove(ctx context.Context, node *Node, publish func(ScanProgress)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.validate(); err != nil {
		return err
	}
	root, err := d.root.OpenRoot(d.relative)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(d.info, info) {
		return errors.New("The folder changed after confirmation; nothing was deleted")
	}
	plan := retainedDeletionPlan(node)
	for pass := 0; pass < 2; pass++ {
		phase := "Deleting"
		if pass != 0 {
			phase = "Deleting remaining items"
		}
		d.runDeletionPlan(ctx, root, plan, phase, publish)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.validate(); err != nil {
			return err
		}
		err = d.root.Remove(d.relative)
		if err == nil || os.IsNotExist(err) {
			if publish != nil {
				publish(ScanProgress{Phase: phase, Path: d.path, Completed: plan.total, Total: plan.total, Fraction: 1})
			}
			return nil
		}
		if pass == 0 {
			if publish != nil {
				publish(ScanProgress{Phase: "Checking remaining items", Path: d.path})
			}
			plan, err = remainingDeletionPlan(ctx, root)
			if err != nil {
				return err
			}
		}
	}
	return err
}

func (d *folderDeletion) runDeletionPlan(ctx context.Context, root *os.Root, plan deletionPlan, phase string, publish func(ScanProgress)) {
	var mu sync.Mutex
	completed := 0
	var last time.Time
	report := func(force bool) {
		mu.Lock()
		defer mu.Unlock()
		if !force {
			completed++
		}
		if publish != nil && (force || completed == 1 || time.Since(last) >= 100*time.Millisecond) {
			last = time.Now()
			publish(ScanProgress{Phase: phase, Path: d.path, Completed: completed, Total: plan.total, Fraction: float64(completed) / float64(max(1, plan.total))})
		}
	}
	report(true)
	jobs := make(chan deletionBatch)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Go(func() {
			var parent *os.Root
			var directory string
			defer func() {
				if parent != nil {
					parent.Close()
				}
			}()
			for batch := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if parent == nil || directory != batch.directory {
					if parent != nil {
						parent.Close()
						parent = nil
					}
					parent, _ = root.OpenRoot(batch.directory)
					directory = batch.directory
				}
				for _, name := range batch.names {
					if ctx.Err() != nil {
						break
					}
					if parent != nil {
						_ = parent.Remove(name)
					}
					report(false)
				}
			}
		})
	}
	for _, batch := range plan.batches {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- batch:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	for i := len(plan.directories) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			break
		}
		if plan.directories[i] != "." {
			_ = root.Remove(plan.directories[i])
			report(false)
		}
	}
	report(true)
}
