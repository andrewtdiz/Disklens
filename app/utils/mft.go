package utils

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf16"

	"www.velocidex.com/golang/go-ntfs/parser"
)

const mftReferenceMask = uint64(1<<48) - 1

type mftRecord struct {
	size          int64
	modified      uint64
	attributes    uint32
	sequence      uint16
	present       bool
	multipleLinks bool
}

type mftLink struct {
	reference uint64
	name      string
}

type mftIndex struct {
	records  []mftRecord
	children map[uint64][]mftLink
}

func scanNTFSImage(ctx context.Context, disk io.ReaderAt, path string, rootReference uint64, progress func(ScanProgress)) (*Scan, error) {
	paged, err := parser.NewPagedReader(disk, 4096, 256)
	if err != nil {
		return nil, err
	}
	ntfs, err := parser.GetNTFSContext(paged, 0, parser.GetDefaultOptions())
	if err != nil {
		return nil, err
	}
	defer ntfs.Close()
	reader, ok := ntfs.MFTReader.(parser.RangeReaderAt)
	if !ok {
		return nil, errors.New("NTFS parser did not expose the MFT extents")
	}
	recordSize := ntfs.GetRecordSize()
	size := parser.RangeSize(reader)
	if recordSize < 512 || recordSize > 65536 || recordSize%512 != 0 || size < recordSize || size%recordSize != 0 {
		return nil, errors.New("Invalid NTFS record geometry")
	}
	if rootReference == 0 {
		root, err := ntfs.GetMFT(5)
		if err != nil {
			return nil, err
		}
		rootReference = 5 | uint64(root.Sequence_value())<<48
	}
	index := mftIndex{records: make([]mftRecord, size/recordSize), children: make(map[uint64][]mftLink)}
	buffer := make([]byte, (4*1024*1024/recordSize)*recordSize)
	var last time.Time
	batches := 0
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		block := buffer[:min(int64(len(buffer)), size-offset)]
		n, err := reader.ReadAt(block, offset)
		if n != len(block) || (err != nil && err != io.EOF) {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("Read MFT at %d: %w", offset, err)
		}
		batches++
		for start := 0; start < len(block); start += int(recordSize) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := index.addRecord(block[start:start+int(recordSize)], uint64((offset+int64(start))/recordSize)); err != nil {
				return nil, err
			}
		}
		offset += int64(len(block))
		if progress != nil && (time.Since(last) >= 100*time.Millisecond || offset == size) {
			last = time.Now()
			progress(ScanProgress{Phase: "Reading NTFS", Path: path, Fraction: float64(offset) / float64(size)})
		}
	}
	if progress != nil {
		progress(ScanProgress{Phase: "Building file tree", Path: path})
	}
	s, err := index.tree(ctx, path, rootReference)
	if err != nil {
		return nil, err
	}
	s.DirectoryBatches = batches
	s.Backend = "NTFS MFT"
	return s, nil
}

func (index *mftIndex) addRecord(b []byte, id uint64) error {
	if string(b[:4]) != "FILE" || binary.LittleEndian.Uint16(b[22:24])&1 == 0 {
		return nil
	}
	used := int(binary.LittleEndian.Uint32(b[24:28]))
	fixup := int(binary.LittleEndian.Uint16(b[4:6]))
	count := int(binary.LittleEndian.Uint16(b[6:8]))
	if used > len(b) || used < 48 || fixup < 8 || fixup+count*2 > len(b) || count != len(b)/512+1 {
		return fmt.Errorf("Invalid MFT record %d", id)
	}
	for i := 1; i < count; i++ {
		end := i*512 - 2
		if binary.LittleEndian.Uint16(b[end:end+2]) != binary.LittleEndian.Uint16(b[fixup:fixup+2]) {
			return fmt.Errorf("MFT record %d changed during the scan", id)
		}
		copy(b[end:end+2], b[fixup+i*2:fixup+i*2+2])
	}
	reference := id | uint64(binary.LittleEndian.Uint16(b[16:18]))<<48
	base := binary.LittleEndian.Uint64(b[32:40])
	if base != 0 {
		reference = base
	}
	recordID := reference & mftReferenceMask
	if recordID >= uint64(len(index.records)) {
		return fmt.Errorf("Invalid MFT base reference %d", recordID)
	}
	r := &index.records[recordID]
	sequence := uint16(reference >> 48)
	if r.sequence != sequence {
		if base != 0 && r.present {
			return nil
		}
		*r = mftRecord{sequence: sequence}
	}
	if base == 0 {
		r.present = true
		r.multipleLinks = binary.LittleEndian.Uint16(b[18:20]) > 1
		if binary.LittleEndian.Uint16(b[22:24])&2 != 0 {
			r.attributes |= 0x10
		}
	}
	offset := int(binary.LittleEndian.Uint16(b[20:22]))
	for offset+4 <= used {
		kind := binary.LittleEndian.Uint32(b[offset : offset+4])
		if kind == 0xffffffff {
			break
		}
		if offset+16 > used {
			return fmt.Errorf("Invalid attribute in MFT record %d", id)
		}
		length := int(binary.LittleEndian.Uint32(b[offset+4 : offset+8]))
		if length < 24 || offset+length > used {
			return fmt.Errorf("Invalid attribute length in MFT record %d", id)
		}
		attr := b[offset : offset+length]
		nonresident := attr[8] != 0
		var value []byte
		if !nonresident {
			start := int(binary.LittleEndian.Uint16(attr[20:22]))
			size := int(binary.LittleEndian.Uint32(attr[16:20]))
			if start < 24 || start+size > length {
				return fmt.Errorf("Invalid resident attribute in MFT record %d", id)
			}
			value = attr[start : start+size]
		}
		switch kind {
		case 0x10:
			if len(value) < 36 {
				return fmt.Errorf("Missing NTFS standard information in record %d", id)
			}
			r.modified = binary.LittleEndian.Uint64(value[8:16])
			r.attributes |= binary.LittleEndian.Uint32(value[32:36])
		case 0x30:
			if len(value) < 66 || 66+int(value[64])*2 > len(value) {
				return fmt.Errorf("Invalid NTFS file name in record %d", id)
			}
			if value[65] != 2 {
				parent := binary.LittleEndian.Uint64(value[:8])
				name := decodeNTFSName(value[66 : 66+int(value[64])*2])
				if name != "." && parent != reference {
					index.children[parent] = append(index.children[parent], mftLink{reference, name})
				}
			}
		case 0x80:
			if attr[9] == 0 {
				if nonresident {
					if length < 64 {
						return fmt.Errorf("Invalid NTFS data attribute in record %d", id)
					}
					if binary.LittleEndian.Uint64(attr[16:24]) == 0 {
						r.size = int64(binary.LittleEndian.Uint64(attr[40:48]))
						if binary.LittleEndian.Uint16(attr[12:14])&0x8001 != 0 {
							if length < 72 {
								return fmt.Errorf("Invalid compressed allocation in record %d", id)
							}
							r.size = int64(binary.LittleEndian.Uint64(attr[64:72]))
						}
					}
				} else {
					r.size = (int64(len(value)) + 7) &^ 7
				}
			}
		}
		offset += length
	}
	return nil
}

func decodeNTFSName(b []byte) string {
	var name [255]uint16
	for i := 0; i < len(b)/2; i++ {
		name[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
	}
	return string(utf16.Decode(name[:len(b)/2]))
}

func (index *mftIndex) tree(ctx context.Context, path string, rootReference uint64) (*Scan, error) {
	rootID := rootReference & mftReferenceMask
	if rootID >= uint64(len(index.records)) {
		return nil, errors.New("Selected directory is absent from the MFT")
	}
	root := index.records[rootID]
	if !root.present || root.sequence != uint16(rootReference>>48) || root.attributes&0x10 == 0 {
		return nil, errors.New("Selected directory changed during the MFT scan")
	}
	s := &Scan{At: time.Now(), Root: &Node{Name: path, Dir: true}}
	seen := make([]bool, len(index.records))
	linkOwners := make(map[uint64]*Node)
	var visit func(*Node, uint64) error
	visit = func(parent *Node, reference uint64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := reference & mftReferenceMask
		if seen[id] {
			return errors.New("Directory cycle in MFT")
		}
		seen[id] = true
		s.Folders++
		record := index.records[id]
		parent.Modified = timestampFromNTFS(record.modified)
		links := index.children[reference]
		sort.Slice(links, func(i, j int) bool { return links[i].name < links[j].name })
		for _, link := range links {
			if err := ctx.Err(); err != nil {
				return err
			}
			childID := link.reference & mftReferenceMask
			if childID >= uint64(len(index.records)) {
				return errors.New("Invalid child MFT reference")
			}
			child := index.records[childID]
			if !child.present || child.sequence != uint16(link.reference>>48) {
				s.RecordError(filepath.Join(parent.FullPath(), link.name), errors.New("File changed during MFT scan"))
				continue
			}
			if child.attributes&0x400 != 0 {
				s.Skipped++
				continue
			}
			n := &Node{Name: link.name, Parent: parent, Dir: child.attributes&0x10 != 0, Modified: timestampFromNTFS(child.modified)}
			parent.addChild(n)
			if n.Dir {
				if err := visit(n, link.reference); err != nil {
					return err
				}
			} else {
				s.Files++
				if child.multipleLinks {
					if previous := linkOwners[childID]; previous != nil {
						retainLinks(previous, n)
					} else {
						linkOwners[childID] = n
					}
				}
				if !seen[childID] {
					n.Size = child.size
					seen[childID] = true
					s.Bytes += n.Size
				}
			}
			parent.Size += n.Size
		}
		sortNodes(parent.Children())
		return nil
	}
	if err := visit(s.Root, rootReference); err != nil {
		return nil, err
	}
	s.Fraction = 1
	return s, nil
}
