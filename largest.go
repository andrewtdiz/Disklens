package main

import (
	"container/heap"
	"context"
)

type fileHeap []*Node

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(value any)    { *h = append(*h, value.(*Node)) }
func (h *fileHeap) Pop() any          { a := *h; last := a[len(a)-1]; *h = a[:len(a)-1]; return last }

func largestFiles(ctx context.Context, root *Node) ([]*Node, error) {
	h := make(fileHeap, 0, 100)
	var visit func(*Node) error
	visit = func(n *Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !n.Dir {
			if len(h) < 100 {
				heap.Push(&h, n)
			} else if n.Size > h[0].Size {
				h[0] = n
				heap.Fix(&h, 0)
			}
		}
		for _, child := range n.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	out := []*Node(h)
	sortNodes(out)
	return out, nil
}
