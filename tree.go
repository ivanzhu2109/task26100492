package backupdir

import (
	"sort"
	"strings"
)

// node 是不可变目录树的节点。整棵树为持久化结构：
// 每次变更只克隆从根到受影响位置的路径，其余子树在各快照间共享。
// isFile 为 true 时表示普通文件；否则为目录，children 为其直接子节点。
// 目录完全由文件路径隐含，空目录不被记录。
type node struct {
	isFile   bool
	children map[string]*node
}

// snapshot 是一个不可修改的目录版本，以其根节点标识。
type snapshot struct {
	root *node
}

// splitPath 将已规范化的相对路径切分为分量。空路径（根）返回空切片。
func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// cloneChildren 返回 children 的浅拷贝，nil 时返回空 map。
func cloneChildren(children map[string]*node) map[string]*node {
	cloned := make(map[string]*node, len(children)+1)
	for name, child := range children {
		cloned[name] = child
	}
	return cloned
}

// applyWrite 在以 n 为根的子树上执行写入：comps 为剩余路径分量。
// 返回新的子树根。若 n 原本是文件而仍需向下深入，该文件被移除并视为空目录。
func applyWrite(n *node, comps []string) *node {
	if len(comps) == 0 {
		// 在该位置写入文件：替换旧文件，并清除全部后代（丢弃 children）。
		return &node{isFile: true}
	}
	var children map[string]*node
	if n != nil && !n.isFile {
		children = cloneChildren(n.children)
	} else {
		// n 为文件（祖先文件被移除）或不存在：从空目录开始。
		children = make(map[string]*node, 1)
	}
	head := comps[0]
	children[head] = applyWrite(n.childAt(head), comps[1:])
	return &node{children: children}
}

// childAt 返回名为 name 的直接子节点，不存在、n 为 nil 或 n 为文件时返回 nil。
func (n *node) childAt(name string) *node {
	if n == nil || n.isFile {
		return nil
	}
	return n.children[name]
}

// applyDelete 在以 n 为根的子树上删除 comps 指向的路径：
// 移除同路径文件及全部后代。路径不存在时不产生变化，返回原节点以保留共享。
func applyDelete(n *node, comps []string) *node {
	if n == nil || n.isFile && len(comps) > 0 {
		// 路径不存在（n 为 nil），或需要在文件内部继续删除（无意义）。
		return n
	}
	if len(comps) == 0 {
		// 删除该位置本身：文件或整个子树一并移除。
		return nil
	}
	head := comps[0]
	oldChild := n.children[head]
	newChild := applyDelete(oldChild, comps[1:])
	if newChild == oldChild {
		return n // 无变化，保留共享。
	}
	children := cloneChildren(n.children)
	if newChild == nil {
		delete(children, head)
	} else {
		children[head] = newChild
	}
	return &node{children: children}
}

// applyChange 在根为 root 的树上应用一条变更，返回新根。
func applyChange(root *node, c Change) *node {
	comps := splitPath(c.Path)
	switch c.Kind {
	case ChangeWrite:
		return applyWrite(root, comps)
	case ChangeDelete:
		return applyDelete(root, comps)
	default:
		return root
	}
}

// collect 将以 n 为根、路径前缀为 prefix 的子树中的全部文件路径
// 按字典序追加到 out。n 为文件时只追加 prefix 本身。
func collect(n *node, prefix string, out []string) []string {
	if n == nil {
		return out
	}
	if n.isFile {
		return append(out, prefix)
	}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		childPrefix := name
		if prefix != "" {
			childPrefix = prefix + "/" + name
		}
		out = collect(n.childAt(name), childPrefix, out)
	}
	return out
}

// lookup 返回 comps 指向的节点，路径不存在时返回 nil。
func (n *node) lookup(comps []string) *node {
	cur := n
	for _, comp := range comps {
		cur = cur.childAt(comp)
		if cur == nil {
			return nil
		}
	}
	return cur
}
