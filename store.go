// Package backupdir 提供增量备份目录库：在进程内存中维护一个端点的
// 一条快照序列，支持创建快照、查询保留快照的目录、保留最近 K 个快照。
// 本模块只管理目录元数据（文件相对路径集合），不读取或存储文件内容。
package backupdir

// ChangeKind 表示一条变更的类型。
type ChangeKind int

const (
	// ChangeWrite 写入文件：替换同路径旧文件，清除该路径下全部后代文件；
	// 若任一祖先路径原本是文件，该祖先文件也被移除。
	ChangeWrite ChangeKind = iota
	// ChangeDelete 删除路径：移除同路径文件及全部后代文件；路径不存在时不产生变化。
	ChangeDelete
)

// Change 表示一条有序变更。Path 为已规范化的相对路径，以斜杠分隔。
type Change struct {
	Kind ChangeKind
	Path string
}

// Store 维护一个端点的一条快照序列。初始目录为空。
// 快照 ID 即快照在序列中的创建序号，从 0 开始递增；
// 淘汰最旧快照后，保留快照的 ID 不变。
type Store struct {
	snapshots []*snapshot
	base      int // snapshots[0] 对应的快照 ID
}

// NewStore 创建一个空的 Store，初始不含任何快照，目录为空。
func NewStore() *Store {
	return &Store{}
}

// CreateSnapshot 基于最新快照按输入顺序应用一批变更，整批生效后产生一个
// 不可修改的新版本，返回新快照的 ID。首个快照基于空目录。
// 已有的全部快照（包括最新快照）保持不变。
func (s *Store) CreateSnapshot(changes []Change) int {
	var base *node
	if n := len(s.snapshots); n > 0 {
		base = s.snapshots[n-1].root
	}
	root := base
	for _, c := range changes {
		root = applyChange(root, c)
	}
	s.snapshots = append(s.snapshots, &snapshot{root: root})
	return s.base + len(s.snapshots) - 1
}

// Query 查询指定快照中的路径：路径是文件时返回仅含该文件的列表；
// 否则返回该目录下全部后代文件；空路径查询根目录。
// 结果为按字典序排序的相对路径列表，是该快照当时的完整状态。
// 快照 ID 无效（不存在或已淘汰）时返回 nil 和 false。
func (s *Store) Query(snapshotID int, path string) ([]string, bool) {
	idx := snapshotID - s.base
	if idx < 0 || idx >= len(s.snapshots) {
		return nil, false
	}
	root := s.snapshots[idx].root
	if path == "" {
		// 根目录始终存在，即使该快照为空目录。
		return collect(root, "", nil), true
	}
	target := root.lookup(splitPath(path))
	if target == nil {
		// 路径不存在：既不是文件也不是（隐含）目录。
		return nil, false
	}
	return collect(target, path, nil), true
}

// Retain 只保留最近 k 个快照，最旧的多余快照立即不可查询；
// 不改变任何保留快照的内容，也不改变其快照 ID。
// k <= 0 时淘汰全部快照；k 大于现有数量时不产生变化。
func (s *Store) Retain(k int) {
	if k < 0 {
		k = 0
	}
	excess := len(s.snapshots) - k
	if excess <= 0 {
		return
	}
	for i := 0; i < excess; i++ {
		s.snapshots[i] = nil // 解除引用，允许回收不再共享的子树。
	}
	s.snapshots = append([]*snapshot(nil), s.snapshots[excess:]...)
	s.base += excess
}

// SnapshotCount 返回当前保留的快照数量。
func (s *Store) SnapshotCount() int {
	return len(s.snapshots)
}
