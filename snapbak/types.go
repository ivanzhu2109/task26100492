// Package snapbak 提供单端点、单序列的增量备份目录库。
// 目录元数据保存在进程内存中，文件内容由上游保管，本包不读取或存储文件内容。
package snapbak

// Op 表示一条变更的类型。
type Op int

const (
	// OpWrite 写入文件：替换同路径旧文件，清除该路径下全部后代文件，
	// 并移除路径上任一是文件的祖先。
	OpWrite Op = iota
	// OpDelete 删除路径：移除同路径文件及全部后代文件；路径不存在时不产生变化。
	OpDelete
)

// Change 是一条目录变更。Path 为已规范化的相对路径，以斜杠分隔，
// 不含空分量、"." 或 ".." 分量，区分大小写。
type Change struct {
	Op   Op
	Path string
}
