package snapbak

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func mustQuery(t *testing.T, s *Store, id int, path string) []string {
	t.Helper()
	got, err := s.Query(id, path)
	if err != nil {
		t.Fatalf("Query(%d, %q) error: %v", id, path, err)
	}
	sort.Strings(got)
	return got
}

func checkQuery(t *testing.T, s *Store, id int, path string, want []string) {
	t.Helper()
	got := mustQuery(t, s, id, path)
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Query(%d, %q) = %v, want %v", id, path, got, want)
	}
}

// 场景一：目录 a 先含 a/x，随后变成文件 a，再写入 a/y。
// 新快照仅留下 a/y，旧快照仍各自正确。
func TestDirToFileAndBack(t *testing.T) {
	s := NewStore()
	snap1 := s.CreateSnapshot([]Change{{OpWrite, "a/x"}})
	snap2 := s.CreateSnapshot([]Change{{OpWrite, "a"}})
	snap3 := s.CreateSnapshot([]Change{{OpWrite, "a/y"}})

	checkQuery(t, s, snap1, "", []string{"a/x"})
	checkQuery(t, s, snap1, "a", []string{"a/x"})

	checkQuery(t, s, snap2, "", []string{"a"})
	checkQuery(t, s, snap2, "a", []string{"a"})

	// 目录被文件遮蔽后重新写入，只能出现新内容，a/x 不得复活。
	checkQuery(t, s, snap3, "", []string{"a/y"})
	checkQuery(t, s, snap3, "a", []string{"a/y"})
	checkQuery(t, s, snap3, "a/x", nil)
}

// 同一批变更严格按输入顺序生效。
func TestBatchOrderMatters(t *testing.T) {
	s := NewStore()
	id := s.CreateSnapshot([]Change{
		{OpWrite, "a/x"},
		{OpWrite, "a"},
		{OpDelete, "a"},
		{OpWrite, "a/y"},
	})
	checkQuery(t, s, id, "", []string{"a/y"})
}

// 删除路径的前缀边界：删除 a 不能影响 ab。
func TestDeletePrefixBoundary(t *testing.T) {
	s := NewStore()
	s.CreateSnapshot([]Change{{OpWrite, "a/x"}, {OpWrite, "ab/y"}})
	id := s.CreateSnapshot([]Change{{OpDelete, "a"}})
	checkQuery(t, s, id, "", []string{"ab/y"})
	checkQuery(t, s, id, "a", nil)
	checkQuery(t, s, id, "ab", []string{"ab/y"})
}

// 删除不存在的路径不产生变化；写入替换同路径旧文件。
func TestDeleteMissingAndOverwrite(t *testing.T) {
	s := NewStore()
	snap1 := s.CreateSnapshot([]Change{{OpWrite, "f"}})
	snap2 := s.CreateSnapshot([]Change{{OpDelete, "no/such"}, {OpWrite, "f"}})
	checkQuery(t, s, snap1, "", []string{"f"})
	checkQuery(t, s, snap2, "", []string{"f"})
	checkQuery(t, s, snap2, "f", []string{"f"})
}

// 淘汰的快照不可查询；k 最小为 1。
func TestRetainBasics(t *testing.T) {
	s := NewStore()
	s.CreateSnapshot([]Change{{OpWrite, "a"}})
	snap2 := s.CreateSnapshot([]Change{{OpWrite, "b"}})
	s.Retain(1)
	if _, err := s.Query(0, ""); err != ErrSnapshotDropped {
		t.Errorf("Query(0) err = %v, want ErrSnapshotDropped", err)
	}
	if _, err := s.Query(1, ""); err != ErrSnapshotDropped {
		t.Errorf("Query(1) err = %v, want ErrSnapshotDropped", err)
	}
	checkQuery(t, s, snap2, "", []string{"a", "b"})
	if got := s.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}
	s.Retain(0) // 至少保留最新快照
	checkQuery(t, s, snap2, "", []string{"a", "b"})
}

// reference 用普通 map 逐快照完整重建，作为对照实现。
type reference struct {
	snaps []map[string]bool
}

func newReference() *reference {
	return &reference{snaps: []map[string]bool{{}}}
}

func (r *reference) apply(changes []Change) {
	cur := make(map[string]bool, len(r.snaps[len(r.snaps)-1]))
	for p := range r.snaps[len(r.snaps)-1] {
		cur[p] = true
	}
	for _, ch := range changes {
		switch ch.Op {
		case OpWrite:
			// 移除该路径下全部后代与任一文件祖先。
			for p := range cur {
				if p == ch.Path || hasPathPrefix(p, ch.Path+"/") || hasPathPrefix(ch.Path, p+"/") {
					delete(cur, p)
				}
			}
			cur[ch.Path] = true
		case OpDelete:
			for p := range cur {
				if p == ch.Path || hasPathPrefix(p, ch.Path+"/") {
					delete(cur, p)
				}
			}
		}
	}
	r.snaps = append(r.snaps, cur)
}

func hasPathPrefix(p, prefix string) bool {
	return len(p) >= len(prefix) && p[:len(prefix)] == prefix
}

func (r *reference) list(id int) []string {
	out := make([]string, 0, len(r.snaps[id]))
	for p := range r.snaps[id] {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// 场景二：淘汰初始完整快照后继续增量写入和删除，
// 所有保留目录与逐快照完整重建的参考结果一致。
func TestRetainThenContinueMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	s := NewStore()
	ref := newReference()

	names := []string{"a", "b", "c", "ab", "x", "y", "z", "d1", "d2"}
	randPath := func() string {
		depth := 1 + rng.Intn(3)
		segs := make([]string, depth)
		for i := range segs {
			segs[i] = names[rng.Intn(len(names))]
		}
		out := segs[0]
		for _, seg := range segs[1:] {
			out += "/" + seg
		}
		return out
	}
	randBatch := func() []Change {
		n := 1 + rng.Intn(5)
		batch := make([]Change, n)
		for i := range batch {
			op := OpWrite
			if rng.Intn(3) == 0 {
				op = OpDelete
			}
			batch[i] = Change{op, randPath()}
		}
		return batch
	}

	var ids []int
	// 先建立若干快照，然后淘汰掉包括初始完整快照在内的旧快照。
	for i := 0; i < 10; i++ {
		batch := randBatch()
		ids = append(ids, s.CreateSnapshot(batch))
		ref.apply(batch)
	}
	s.Retain(3)
	ids = ids[len(ids)-3:]

	// 淘汰后继续增量写入和删除。
	for i := 0; i < 10; i++ {
		batch := randBatch()
		ids = append(ids, s.CreateSnapshot(batch))
		ref.apply(batch)
	}

	// 所有保留快照与参考结果一致；根目录与子目录查询都核对。
	for _, id := range ids {
		checkQuery(t, s, id, "", ref.list(id))
		for _, p := range ref.list(id) {
			checkQuery(t, s, id, p, []string{p})
		}
	}
	// 被淘汰的快照不可查询。
	if _, err := s.Query(0, ""); err != ErrSnapshotDropped {
		t.Errorf("Query(0) err = %v, want ErrSnapshotDropped", err)
	}
}

// 子目录查询返回该目录下全部后代文件。
func TestQuerySubdir(t *testing.T) {
	s := NewStore()
	id := s.CreateSnapshot([]Change{
		{OpWrite, "d/a"}, {OpWrite, "d/sub/b"}, {OpWrite, "e/c"},
	})
	checkQuery(t, s, id, "d", []string{"d/a", "d/sub/b"})
	checkQuery(t, s, id, "d/sub", []string{"d/sub/b"})
	checkQuery(t, s, id, "e", []string{"e/c"})
	checkQuery(t, s, id, "missing", nil)
}

// 大量随机操作下与参考实现持续一致（含多次 Retain）。
func TestRandomAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	s := NewStore()
	ref := newReference()
	var ids []int

	for round := 0; round < 30; round++ {
		var batch []Change
		for i := 0; i < 1+rng.Intn(8); i++ {
			p := fmt.Sprintf("n%d/n%d/f%d", rng.Intn(4), rng.Intn(4), rng.Intn(6))
			if rng.Intn(4) == 0 {
				p = fmt.Sprintf("n%d", rng.Intn(4))
			}
			op := OpWrite
			if rng.Intn(3) == 0 {
				op = OpDelete
			}
			batch = append(batch, Change{op, p})
		}
		ids = append(ids, s.CreateSnapshot(batch))
		ref.apply(batch)
		if round%5 == 4 {
			keep := 1 + rng.Intn(4)
			s.Retain(keep)
			if len(ids) > keep {
				ids = ids[len(ids)-keep:]
			}
		}
		for _, id := range ids {
			checkQuery(t, s, id, "", ref.list(id))
		}
	}
}
