package backupdir

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func write(path string) Change { return Change{Kind: ChangeWrite, Path: path} }
func del(path string) Change   { return Change{Kind: ChangeDelete, Path: path} }

// mustQuery 查询快照并断言成功。
func mustQuery(t *testing.T, s *Store, id int, path string) []string {
	t.Helper()
	got, ok := s.Query(id, path)
	if !ok {
		t.Fatalf("Query(%d, %q) 返回 ok=false，期望成功", id, path)
	}
	return got
}

// assertQuery 断言查询结果与期望的文件列表一致（顺序无关）。
func assertQuery(t *testing.T, s *Store, id int, path string, want ...string) {
	t.Helper()
	got := mustQuery(t, s, id, path)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("Query(%d, %q) = %v，期望 %v", id, path, got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("Query(%d, %q) 结果未排序: %v", id, path, got)
	}
}

// TestDirBecomesFileThenDirAgain 验证需求场景：
// 目录 a 先含 a/x，随后变成文件 a，再写入 a/y 时，
// 新快照仅留下 a/y，旧快照仍各自正确。
func TestDirBecomesFileThenDirAgain(t *testing.T) {
	s := NewStore()
	id0 := s.CreateSnapshot([]Change{write("a/x")})
	id1 := s.CreateSnapshot([]Change{write("a")})
	id2 := s.CreateSnapshot([]Change{write("a/y")})

	// 旧快照各自正确。
	assertQuery(t, s, id0, "", "a/x")
	assertQuery(t, s, id0, "a", "a/x")
	assertQuery(t, s, id0, "a/x", "a/x")

	assertQuery(t, s, id1, "", "a")
	assertQuery(t, s, id1, "a", "a")

	// 新快照仅留下 a/y，被遮蔽的 a/x 不重新出现，文件 a 也被移除。
	assertQuery(t, s, id2, "", "a/y")
	assertQuery(t, s, id2, "a", "a/y")
	assertQuery(t, s, id2, "a/y", "a/y")
}

// TestBatchOrdering 验证同一批变更严格按输入顺序生效：
// 目录被删除后再在其下写入，只出现重新写入的内容。
func TestBatchOrdering(t *testing.T) {
	s := NewStore()
	s.CreateSnapshot([]Change{write("d/1"), write("d/2"), write("d/sub/3")})
	id := s.CreateSnapshot([]Change{del("d"), write("d/new")})
	assertQuery(t, s, id, "", "d/new")

	// 目录变成文件后再写入其下，仅保留新写入内容。
	id2 := s.CreateSnapshot([]Change{write("d"), write("d/another")})
	assertQuery(t, s, id2, "", "d/another")
}

// TestWriteClearsDescendantsAndAncestorFiles 验证写入的清除语义。
func TestWriteClearsDescendantsAndAncestorFiles(t *testing.T) {
	s := NewStore()
	s.CreateSnapshot([]Change{write("a/b/c"), write("a/b/d"), write("keep")})
	// 写入 a/b：清除其下全部后代。
	id := s.CreateSnapshot([]Change{write("a/b")})
	assertQuery(t, s, id, "", "a/b", "keep")
	// 在文件 a/b 下写入 a/b/e：祖先文件 a/b 被移除。
	id2 := s.CreateSnapshot([]Change{write("a/b/e")})
	assertQuery(t, s, id2, "", "a/b/e", "keep")
}

// TestDeleteSemantics 验证删除语义：删除不存在路径无变化；
// 删除 "a" 不影响 "ab"；删除目录移除全部后代。
func TestDeleteSemantics(t *testing.T) {
	s := NewStore()
	s.CreateSnapshot([]Change{write("a/x"), write("ab/y"), write("a")})
	// 上批先写 a/x 再写 a：a 变成文件，a/x 被清除。
	id := s.CreateSnapshot([]Change{del("a")})
	assertQuery(t, s, id, "", "ab/y")

	// 删除不存在的路径不产生变化。
	id2 := s.CreateSnapshot([]Change{del("no/such/path")})
	assertQuery(t, s, id2, "", "ab/y")

	// 删除根目录下的子树。
	id3 := s.CreateSnapshot([]Change{write("dir/p"), write("dir/q"), write("dir/r/s")})
	id4 := s.CreateSnapshot([]Change{del("dir")})
	assertQuery(t, s, id3, "dir", "dir/p", "dir/q", "dir/r/s")
	assertQuery(t, s, id4, "", "ab/y")
}

// TestQueryNotFound 验证无效快照 ID 与不存在路径的查询。
func TestQueryNotFound(t *testing.T) {
	s := NewStore()
	id := s.CreateSnapshot([]Change{write("f")})
	if _, ok := s.Query(id+1, ""); ok {
		t.Fatal("查询不存在的快照应返回 ok=false")
	}
	if _, ok := s.Query(id, "missing"); ok {
		t.Fatal("查询不存在的路径应返回 ok=false")
	}
	// 空快照的根目录可查询，结果为空。
	empty := NewStore()
	eid := empty.CreateSnapshot(nil)
	assertQuery(t, empty, eid, "")
}

// TestRetain 验证保留操作：最旧快照立即不可查询，保留快照内容与 ID 不变。
func TestRetain(t *testing.T) {
	s := NewStore()
	id0 := s.CreateSnapshot([]Change{write("a")})
	id1 := s.CreateSnapshot([]Change{write("b")})
	id2 := s.CreateSnapshot([]Change{write("c")})

	s.Retain(2)
	if got := s.SnapshotCount(); got != 2 {
		t.Fatalf("SnapshotCount() = %d，期望 2", got)
	}
	if _, ok := s.Query(id0, ""); ok {
		t.Fatal("已淘汰的快照应不可查询")
	}
	assertQuery(t, s, id1, "", "a", "b")
	assertQuery(t, s, id2, "", "a", "b", "c")

	// 淘汰后新快照 ID 继续递增，不与保留快照冲突。
	id3 := s.CreateSnapshot([]Change{del("a")})
	if id3 != id2+1 {
		t.Fatalf("新快照 ID = %d，期望 %d", id3, id2+1)
	}
	assertQuery(t, s, id3, "", "b", "c")

	// k 大于现有数量时不产生变化；k=0 淘汰全部。
	s.Retain(10)
	if got := s.SnapshotCount(); got != 3 {
		t.Fatalf("SnapshotCount() = %d，期望 3", got)
	}
	s.Retain(0)
	if got := s.SnapshotCount(); got != 0 {
		t.Fatalf("SnapshotCount() = %d，期望 0", got)
	}
	// 全部淘汰后仍可继续创建快照。
	id4 := s.CreateSnapshot([]Change{write("z")})
	assertQuery(t, s, id4, "", "z")
}

// reference 是逐快照完整重建的参考模型：直接维护文件路径集合。
type reference struct {
	files map[string]bool
}

func newReference() *reference { return &reference{files: map[string]bool{}} }

func (r *reference) clone() *reference {
	n := &reference{files: make(map[string]bool, len(r.files))}
	for f := range r.files {
		n.files[f] = true
	}
	return n
}

func (r *reference) apply(c Change) {
	switch c.Kind {
	case ChangeWrite:
		// 祖先文件被移除。
		parts := strings.Split(c.Path, "/")
		for i := 1; i < len(parts); i++ {
			delete(r.files, strings.Join(parts[:i], "/"))
		}
		// 同路径旧文件及全部后代被清除。
		r.removeAtOrUnder(c.Path)
		r.files[c.Path] = true
	case ChangeDelete:
		r.removeAtOrUnder(c.Path)
	}
}

func (r *reference) removeAtOrUnder(path string) {
	prefix := path + "/"
	for f := range r.files {
		if f == path || strings.HasPrefix(f, prefix) {
			delete(r.files, f)
		}
	}
}

func (r *reference) sorted() []string {
	out := make([]string, 0, len(r.files))
	for f := range r.files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// TestAgainstReference 随机生成变更序列，验证：
// 淘汰最初的完整快照后继续增量写入和删除，
// 所有保留目录与逐快照完整重建的参考结果一致。
func TestAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261004))
	s := NewStore()

	var ids []int
	var refs []*reference
	cur := newReference()

	// 首个快照为完整快照（大批量写入）。
	var full []Change
	for i := 0; i < 50; i++ {
		full = append(full, write(fmt.Sprintf("dir%d/file%d", rng.Intn(5), i)))
	}
	cur2 := cur.clone()
	for _, c := range full {
		cur2.apply(c)
	}
	cur = cur2
	ids = append(ids, s.CreateSnapshot(full))
	refs = append(refs, cur)

	// 路径池：包含共享前缀、相邻名（a 与 ab）、深层路径。
	pool := []string{
		"a", "a/x", "a/y", "ab", "ab/x", "dir0/file0", "dir0/file1",
		"dir1/file9", "dir2/sub/deep", "dir2/sub", "x", "x/y/z",
	}
	randomChange := func() Change {
		p := pool[rng.Intn(len(pool))]
		if rng.Intn(3) == 0 {
			return del(p)
		}
		return write(p)
	}

	// 逐轮：创建增量快照，间或执行 Retain，每轮校验全部保留快照。
	for round := 0; round < 60; round++ {
		n := rng.Intn(6)
		batch := make([]Change, n)
		next := cur.clone()
		for i := range batch {
			batch[i] = randomChange()
			next.apply(batch[i])
		}
		cur = next
		ids = append(ids, s.CreateSnapshot(batch))
		refs = append(refs, cur)

		if round%5 == 4 {
			keep := 1 + rng.Intn(len(ids))
			s.Retain(keep)
			drop := len(ids) - keep
			if drop < 0 {
				drop = 0
			}
			ids = ids[drop:]
			refs = refs[drop:]
		}

		for i, id := range ids {
			got := mustQuery(t, s, id, "")
			want := refs[i].sorted()
			if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
				t.Fatalf("第 %d 轮：快照 %d 根目录 = %v，参考结果 %v", round, id, got, want)
			}
			// 抽查子目录与文件查询。
			if len(want) > 0 {
				f := want[rng.Intn(len(want))]
				assertQuery(t, s, id, f, f)
			}
		}
	}

	// 淘汰到只剩最近 3 个（最初的完整快照早已淘汰），
	// 继续增量写入和删除，仍与参考一致。
	s.Retain(3)
	ids = ids[len(ids)-3:]
	refs = refs[len(refs)-3:]
	for round := 0; round < 20; round++ {
		batch := []Change{randomChange(), randomChange()}
		next := cur.clone()
		for _, c := range batch {
			next.apply(c)
		}
		cur = next
		ids = append(ids, s.CreateSnapshot(batch))
		refs = append(refs, cur)
	}
	for i, id := range ids {
		got := mustQuery(t, s, id, "")
		want := refs[i].sorted()
		if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("淘汰后：快照 %d 根目录 = %v，参考结果 %v", id, got, want)
		}
	}
}
