package snapbak

// node 是不可变的目录树节点，通过路径复制（copy-on-write）实现结构共享，
// 使各快照之间共享未修改的子树。file 为 true 时该节点是文件，无子节点。
type node struct {
	file     bool
	children map[string]*node
}

// clone 浅拷贝节点及其 children 映射（子节点本身仍共享）。
func (n *node) clone() *node {
	c := &node{file: n.file}
	if len(n.children) > 0 {
		c.children = make(map[string]*node, len(n.children))
		for name, child := range n.children {
			c.children[name] = child
		}
	}
	return c
}

// writeFile 返回在路径 segments 处写入文件后的新子树根。
// 沿途任一文件祖先被替换为目录；目标处的旧文件与全部后代被丢弃。
func writeFile(n *node, segments []string) *node {
	if len(segments) == 0 {
		return &node{file: true}
	}
	c := &node{}
	if n != nil && !n.file && len(n.children) > 0 {
		c.children = make(map[string]*node, len(n.children))
		for name, child := range n.children {
			c.children[name] = child
		}
	} else {
		c.children = make(map[string]*node, 1)
	}
	c.children[segments[0]] = writeFile(c.children[segments[0]], segments[1:])
	return c
}

// deletePath 返回删除路径 segments 后的新子树根；路径不存在时返回原节点。
// 删除会移除同路径文件及全部后代文件。
func deletePath(n *node, segments []string) *node {
	if n == nil || n.file {
		return n
	}
	child, ok := n.children[segments[0]]
	if !ok {
		return n
	}
	c := n.clone()
	if len(segments) == 1 {
		delete(c.children, segments[0])
	} else {
		newChild := deletePath(child, segments[1:])
		if newChild == child {
			return n
		}
		if newChild == nil || (!newChild.file && len(newChild.children) == 0) {
			delete(c.children, segments[0])
		} else {
			c.children[segments[0]] = newChild
		}
	}
	return c
}

// collect 将以 prefix 为路径的子树中全部文件追加到 out。
func collect(n *node, prefix string, out []string) []string {
	if n == nil {
		return out
	}
	if n.file {
		return append(out, prefix)
	}
	for name, child := range n.children {
		p := name
		if prefix != "" {
			p = prefix + "/" + name
		}
		out = collect(child, p, out)
	}
	return out
}
