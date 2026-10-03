# backupdir — 增量备份目录库

`backupdir` 是一个 Go 库，由上层备份与恢复流程直接调用，在进程内存中维护
**一个端点的一条快照序列**。库只管理目录元数据（普通文件的相对路径集合），
不读取或存储文件内容；文件内容由上游保管。

## 特性

- 初始目录为空；每个新快照基于最新快照应用一批**有序变更**，整批生效后产生
  一个不可修改的新版本。
- 目录由文件路径隐含，不记录空目录；路径区分大小写，以斜杠分隔。
- 内部使用**持久化（不可变）目录树**：每次变更只克隆从根到受影响位置的路径，
  未受影响的子树在所有快照间共享，因此创建快照与淘汰旧快照都不会改变
  任何保留快照的内容。
- 淘汰最初的完整快照后，剩余快照仍可查询，并可继续以最新快照创建增量版本。

## 变更语义

- `ChangeWrite`（写入文件）：替换同路径旧文件，并清除该路径下全部后代文件；
  若任一祖先路径原本是文件，该祖先文件也被移除。
- `ChangeDelete`（删除路径）：移除同路径文件及全部后代文件；路径不存在时
  不产生变化。按路径分量匹配，删除 `"a"` 不影响 `"ab"`。
- 同一批变更严格按输入顺序生效：目录被删除或变成文件后再在其下写入新文件，
  只会出现重新写入的内容，先前被遮蔽的后代不会重新出现。

## API 使用

```go
package main

import (
	"fmt"

	"backupdir"
)

func main() {
	s := backupdir.NewStore()

	// 创建快照：基于最新快照（首个基于空目录）应用一批有序变更。
	id0 := s.CreateSnapshot([]backupdir.Change{
		{Kind: backupdir.ChangeWrite, Path: "a/x"},
		{Kind: backupdir.ChangeWrite, Path: "b"},
	})
	id1 := s.CreateSnapshot([]backupdir.Change{
		{Kind: backupdir.ChangeDelete, Path: "a"},
		{Kind: backupdir.ChangeWrite, Path: "a/y"},
	})

	// 查询：路径是文件时返回仅含该文件的列表；
	// 是目录时返回其下全部后代文件；空路径查询根目录。
	files, ok := s.Query(id1, "")   // ["a/y" "b"], true
	fmt.Println(files, ok)
	files, ok = s.Query(id0, "a")   // ["a/x"], true（旧快照不受后续修改影响）
	fmt.Println(files, ok)

	// 只保留最近 1 个快照：id0 立即不可查询，id1 内容不变，
	// 之后仍可以 id1 为基创建新的增量快照。
	s.Retain(1)
	if _, ok := s.Query(id0, ""); !ok {
		fmt.Println("id0 已淘汰")
	}
}
```

### API 一览

| 函数 | 说明 |
| --- | --- |
| `NewStore() *Store` | 创建空 Store（初始目录为空，无快照）。 |
| `CreateSnapshot(changes []Change) int` | 基于最新快照按顺序应用变更，返回新快照 ID（从 0 递增，淘汰后不复用）。 |
| `Query(snapshotID int, path string) ([]string, bool)` | 查询快照中的文件或目录；`path` 为空查询根目录。ID 无效或路径不存在时返回 `false`。结果按字典序排序。 |
| `Retain(k int)` | 只保留最近 `k` 个快照，最旧的多余快照立即不可查询；`k <= 0` 淘汰全部。 |
| `SnapshotCount() int` | 返回当前保留的快照数量。 |

## 运行测试

```sh
go test ./...
```

测试覆盖：目录与文件互换（`a/x` → 文件 `a` → `a/y`）、批内顺序、
写入清除后代与祖先文件、删除语义（`a` 与 `ab` 互不影响）、保留/淘汰，
以及与“逐快照完整重建”参考模型对比的随机化测试（含淘汰初始完整快照后
继续增量写入和删除的场景）。

## 文件结构

- `store.go` — 公开 API：`Store`、`Change`、快照的创建/查询/保留。
- `tree.go` — 内部持久化目录树：不可变节点、变更应用与遍历。
- `store_test.go` — 自动化测试。
