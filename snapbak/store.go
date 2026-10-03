package snapbak

import (
	"errors"
	"sort"
	"strings"
)

// ErrSnapshotDropped 表示查询的快照已被保留策略淘汰。
var ErrSnapshotDropped = errors.New("snapbak: snapshot has been dropped")

// Store 管理一个端点的一条快照序列，初始目录为空。
// 快照编号从 0 开始连续递增；编号 0 是初始空目录。
type Store struct {
	roots []*node // roots[i] 为快照 base+i 的根节点
	base  int     // 已被淘汰的快照数量
}

// NewStore 创建一个空的 Store。
func NewStore() *Store {
	return &Store{roots: []*node{{}}}
}

// CreateSnapshot 基于最新快照按输入顺序应用一批变更，
// 整批生效后产生一个不可修改的新版本，返回新快照的编号。
func (s *Store) CreateSnapshot(changes []Change) int {
	root := s.roots[len(s.roots)-1]
	for _, ch := range changes {
		segments := strings.Split(ch.Path, "/")
		switch ch.Op {
		case OpWrite:
			root = writeFile(root, segments)
		case OpDelete:
			root = deletePath(root, segments)
		}
	}
	s.roots = append(s.roots, root)
	return s.base + len(s.roots) - 1
}

// Query 查询快照 id 中的路径：路径是文件时返回该文件，
// 否则返回该目录下全部后代文件；path 为空字符串时查询根目录。
// 结果按字典序排序，且只反映该快照当时的状态。
func (s *Store) Query(id int, path string) ([]string, error) {
	idx := id - s.base
	if idx < 0 || idx >= len(s.roots) {
		return nil, ErrSnapshotDropped
	}
	n := s.roots[idx]
	if path != "" {
		for _, seg := range strings.Split(path, "/") {
			if n == nil || n.file {
				return nil, nil
			}
			n = n.children[seg]
		}
	}
	if n == nil {
		return nil, nil
	}
	if n.file {
		return []string{path}, nil
	}
	out := collect(n, path, nil)
	sort.Strings(out)
	return out, nil
}

// Retain 只保留最近 k 个快照，最旧的多余快照立即不可查询，
// 但不改变任何保留快照的内容。k 最小为 1，
// 保证始终能基于最新快照继续创建增量版本。
func (s *Store) Retain(k int) {
	if k < 1 {
		k = 1
	}
	if len(s.roots) <= k {
		return
	}
	drop := len(s.roots) - k
	for i := 0; i < drop; i++ {
		s.roots[i] = nil
	}
	s.roots = s.roots[drop:]
	s.base += drop
}

// Len 返回当前保留的快照数量。
func (s *Store) Len() int {
	return len(s.roots)
}
