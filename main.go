package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type node struct {
	name     string
	relBase  string // relative to base path (OS separators)
	isDir    bool
	children []*node
	childMap map[string]*node

	parent   *node
	depth    int
	expanded bool

	selected bool // only meaningful for files
}

func newDir(parent *node, name, relBase string) *node {
	n := &node{
		name:     name,
		relBase:  relBase,
		isDir:    true,
		childMap: make(map[string]*node),
		parent:   parent,
	}
	if parent != nil {
		n.depth = parent.depth + 1
	}
	return n
}

func newFile(parent *node, name, relBase string) *node {
	n := &node{
		name:     name,
		relBase:  relBase,
		isDir:    false,
		childMap: nil,
		parent:   parent,
	}
	if parent != nil {
		n.depth = parent.depth + 1
	}
	return n
}

func (n *node) child(name string) (*node, bool) {
	if n.childMap == nil {
		return nil, false
	}
	c, ok := n.childMap[name]
	return c, ok
}

func (n *node) addChild(c *node) {
	if n.childMap == nil {
		panic("addChild on file node")
	}
	n.childMap[c.name] = c
	n.children = append(n.children, c)
}

func finalizeTree(n *node) {
	if !n.isDir {
		return
	}
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.isDir != b.isDir {
			return a.isDir // dirs first
		}
		return a.name < b.name
	})
	for _, c := range n.children {
		finalizeTree(c)
	}
	// Collapse by default if more than 32 immediate elements.
	n.expanded = len(n.children) <= 32
}

func flattenVisible(root *node) []*node {
	var out []*node
	var walk func(*node)
	walk = func(n *node) {
		out = append(out, n)
		if n.isDir && n.expanded {
			for _, c := range n.children {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

func indexOf(nodes []*node, target *node) int {
	for i := range nodes {
		if nodes[i] == target {
			return i
		}
	}
	return 0
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Right   key.Binding
	Left    key.Binding
	Toggle  key.Binding
	Confirm key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Toggle, k.Confirm, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Toggle, k.Confirm, k.Quit},
	}
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Right: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "expand"),
		),
		Left: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "collapse"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "toggle"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "build"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc"),
			key.WithHelp("q/esc", "quit"),
		),
	}
}

type model struct {
	root   *node
	vis    []*node
	cursor int
	offset int

	width  int
	height int

	base        string
	inRepo      bool
	allowBinary bool

	selectedCount int

	keys keyMap
	help help.Model

	aborted   bool
	confirmed bool
}

func newModel(root *node, base string, inRepo bool, allowBinary bool) model {
	m := model{
		root:        root,
		vis:         flattenVisible(root),
		base:        base,
		inRepo:      inRepo,
		allowBinary: allowBinary,
		keys:        defaultKeyMap(),
		help:        help.New(),
	}
	return m
}

func (m model) Init() tea.Cmd { return nil }

func (m *model) viewportHeight() int {
	h := m.height - 2 // 1 status line + 1 help line
	if h < 1 {
		return 1
	}
	return h
}

func (m *model) ensureCursorVisible() {
	vh := m.viewportHeight()

	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.vis) {
		m.cursor = len(m.vis) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	}

	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vh {
		m.offset = m.cursor - vh + 1
	}
	maxOffset := len(m.vis) - vh
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.ensureCursorVisible()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.aborted = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
			m.ensureCursorVisible()
			return m, nil

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.vis)-1 {
				m.cursor++
			}
			m.ensureCursorVisible()
			return m, nil

		case key.Matches(msg, m.keys.Right):
			n := m.vis[m.cursor]
			if n.isDir && !n.expanded && len(n.children) > 0 {
				n.expanded = true
				old := n
				m.vis = flattenVisible(m.root)
				m.cursor = indexOf(m.vis, old)
				m.ensureCursorVisible()
			}
			return m, nil

		case key.Matches(msg, m.keys.Left):
			n := m.vis[m.cursor]
			if n.isDir && n.expanded && len(n.children) > 0 {
				n.expanded = false
				old := n
				m.vis = flattenVisible(m.root)
				m.cursor = indexOf(m.vis, old)
				m.ensureCursorVisible()
			}
			return m, nil

		case key.Matches(msg, m.keys.Toggle):
			n := m.vis[m.cursor]
			if !n.isDir {
				n.selected = !n.selected
				if n.selected {
					m.selectedCount++
				} else {
					m.selectedCount--
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.Confirm):
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	if len(m.vis) == 0 {
		return ""
	}

	mode := "fs"
	if m.inRepo {
		mode = "git"
	}
	bin := "text"
	if m.allowBinary {
		bin = "text+bin"
	}
	status := fmt.Sprintf("%s | %s | selected=%d", mode, bin, m.selectedCount)

	vh := m.viewportHeight()
	start := m.offset
	end := start + vh
	if end > len(m.vis) {
		end = len(m.vis)
	}

	var b strings.Builder
	b.WriteString(status)
	b.WriteByte('\n')

	for i := start; i < end; i++ {
		n := m.vis[i]
		cur := " "
		if i == m.cursor {
			cur = ">"
		}
		indent := strings.Repeat("  ", n.depth)

		if n.isDir {
			icon := "▸"
			if n.expanded {
				icon = "▾"
			}
			fmt.Fprintf(&b, "%s%s%s %s/\n", cur, indent, icon, n.name)
			continue
		}

		box := "[ ]"
		if n.selected {
			box = "[x]"
		}
		fmt.Fprintf(&b, "%s%s%s %s\n", cur, indent, box, n.name)
	}

	b.WriteString(m.help.View(m.keys))
	return b.String()
}

func findGitRoot(start string) (string, bool) {
	dir := start
	for {
		gitPath := filepath.Join(dir, ".git")
		if st, err := os.Stat(gitPath); err == nil && st.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func gitListFiles(base string, startRelSlash string) []string {
	args := []string{
		"-C", base,
		"ls-files",
		"-z",
		"--cached",
		"--others",
		"--exclude-standard",
		"--full-name",
	}
	if startRelSlash != "." {
		args = append(args, "--", startRelSlash)
	}

	out, err := exec.Command("git", args...).Output()
	if err != nil {
		panic(err)
	}

	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		files = append(files, string(p)) // already slash-separated
	}
	return files
}

func walkFiles(base string) []string {
	var files []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		panic(err)
	}
	return files
}

// Heuristic binary detection (cheap). Good enough for gating selection.
// Panic on unexpected errors per requirements.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		panic(err)
	}
	b := buf[:n]
	if len(b) == 0 {
		return false
	}
	if bytes.IndexByte(b, 0) != -1 {
		return true
	}
	// If it looks like UTF-8, treat as text.
	if utf8.Valid(b) {
		return false
	}
	// Otherwise, fall back to control-character ratio.
	ctrl := 0
	for _, c := range b {
		switch c {
		case '\n', '\r', '\t', '\f':
			continue
		default:
			if c < 0x20 || c == 0x7f {
				ctrl++
			}
		}
	}
	return float64(ctrl)/float64(len(b)) > 0.10
}

func buildTree(startRelSlash string, baseRelSlashFiles []string) *node {
	rootRelOS := filepath.FromSlash(startRelSlash)
	root := newDir(nil, startRelSlash, rootRelOS)

	for _, full := range baseRelSlashFiles {
		within := full
		if startRelSlash != "." {
			prefix := startRelSlash + "/"
			if !strings.HasPrefix(full, prefix) {
				// Shouldn't happen if we used pathspec, but keep it strict.
				continue
			}
			within = strings.TrimPrefix(full, prefix)
		}

		if within == "" {
			continue
		}

		parts := strings.Split(within, "/")
		cur := root
		for i := 0; i < len(parts); i++ {
			part := parts[i]
			isLast := i == len(parts)-1

			if !isLast {
				if child, ok := cur.child(part); ok {
					cur = child
					continue
				}
				rel := filepath.Join(cur.relBase, filepath.FromSlash(part))
				d := newDir(cur, part, rel)
				cur.addChild(d)
				cur = d
				continue
			}

			// file leaf
			if _, ok := cur.child(part); ok {
				continue
			}
			rel := filepath.Join(cur.relBase, filepath.FromSlash(part))
			f := newFile(cur, part, rel)
			cur.addChild(f)
		}
	}

	finalizeTree(root)
	return root
}

func (m model) selectedFiles() []string {
	var out []string
	var walk func(*node)
	walk = func(n *node) {
		if !n.isDir && n.selected {
			out = append(out, filepath.ToSlash(n.relBase))
		}
		if n.isDir {
			for _, c := range n.children {
				walk(c)
			}
		}
	}
	walk(m.root)
	sort.Strings(out)
	return out
}

func languageFor(relOS string) string {
	base := filepath.Base(relOS)
	switch base {
	case "Dockerfile":
		return "dockerfile"
	case "Makefile":
		return "make"
	}

	ext := strings.ToLower(filepath.Ext(relOS))
	if ext == "" {
		return ""
	}
	ext = strings.TrimPrefix(ext, ".")
	switch ext {
	case "go":
		return "go"
	case "md", "markdown":
		return "markdown"
	case "txt":
		return "text"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "toml":
		return "toml"
	case "sh", "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "py":
		return "python"
	case "rb":
		return "ruby"
	case "php":
		return "php"
	case "java":
		return "java"
	case "kt":
		return "kotlin"
	case "rs":
		return "rust"
	case "c":
		return "c"
	case "h":
		return "c"
	case "cc", "cpp", "cxx":
		return "cpp"
	case "hpp", "hh":
		return "cpp"
	case "cs":
		return "csharp"
	case "html", "htm":
		return "html"
	case "css":
		return "css"
	case "scss":
		return "scss"
	case "sql":
		return "sql"
	case "xml":
		return "xml"
	case "ini", "conf":
		return "ini"
	default:
		return ext
	}
}

func maxRunByteInReader(r io.Reader, b byte) int {
	buf := make([]byte, 32*1024)
	maxRun := 0
	run := 0

	for {
		n, err := r.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == b {
					run++
					if run > maxRun {
						maxRun = run
					}
				} else {
					run = 0
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
	}
	return maxRun
}

func maxRunByteInFile(path string, b byte) int {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	return maxRunByteInReader(f, b)
}

func fenceForContent(maxRun int) string {
	n := maxRun + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

func buildMarkdown(base string, selectedRelSlash []string, allowBinary bool) (absOut string, size int64, tokens int64) {
	outDir := filepath.Join(base, ".mkctx")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}

	name := fmt.Sprintf("source-context-%s.md", time.Now().Format("2006-01-02-15-04-05"))
	outPath := filepath.Join(outDir, name)

	f, err := os.Create(outPath)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			//panic(err)
		}
	}()

	w := bufio.NewWriter(f)
	defer func() {
		if err := w.Flush(); err != nil {
			panic(err)
		}
	}()

	for _, relSlash := range selectedRelSlash {
		relOS := filepath.FromSlash(relSlash)
		abs := filepath.Join(base, relOS)

		fmt.Fprintf(w, "## %s\n\n", relSlash)

		if allowBinary && isBinary(abs) {
			// Binary file -> `file <relative/path>` output
			cmd := exec.Command("file", relSlash)
			cmd.Dir = base
			out, err := cmd.CombinedOutput()
			if err != nil {
				panic(err)
			}

			fmt.Fprintln(w, "```")
			// Preserve stdout exactly (minus trailing newlines to avoid extra empty lines).
			out = bytes.TrimRight(out, "\n")
			maxRun := 0
			{
				// ищем максимальную серию '`' в выводе file
				run := 0
				for _, c := range out {
					if c == '`' {
						run++
						if run > maxRun {
							maxRun = run
						}
					} else {
						run = 0
					}
				}
			}
			fence := fenceForContent(maxRun)

			fmt.Fprintln(w, fence)
			if len(out) > 0 {
				if _, err := w.Write(out); err != nil {
					panic(err)
				}
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, fence)
			fmt.Fprintln(w)
			continue
		}

		// Text file -> embed contents
		maxRun := maxRunByteInFile(abs, '`')
		fence := fenceForContent(maxRun)

		lang := languageFor(relOS)
		if lang != "" {
			fmt.Fprintf(w, "%s%s\n", fence, lang)
		} else {
			fmt.Fprintln(w, fence)
		}

		in, err := os.Open(abs)
		if err != nil {
			panic(err)
		}
		_, err = io.Copy(w, in)
		_ = in.Close()
		if err != nil {
			panic(err)
		}

		fmt.Fprintln(w)
		fmt.Fprintln(w, fence)
		fmt.Fprintln(w)
	}

	if err := w.Flush(); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}

	st, err := os.Stat(outPath)
	if err != nil {
		panic(err)
	}

	abs, err := filepath.Abs(outPath)
	if err != nil {
		panic(err)
	}

	size = st.Size()
	// Simple estimate: ~4 bytes per token (script-friendly integer).
	tokens = (size + 3) / 4
	return abs, size, tokens
}

func main() {
	allowBinary := flag.Bool("b", false, "allow selecting binary files (use `file <relpath>` in output)")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	base, inRepo := findGitRoot(cwd)
	if !inRepo {
		base = cwd
	}

	//startRelOS := "." // FIXME
	startRelSlash := "."
	if inRepo {
		rel, err := filepath.Rel(base, cwd)
		if err != nil {
			panic(err)
		}
		//startRelOS = rel
		startRelSlash = filepath.ToSlash(rel)
	}

	// Build file list (base-relative slash paths), restricted to current directory.
	var files []string
	if inRepo {
		files = gitListFiles(base, startRelSlash)
	} else {
		files = walkFiles(base)
	}

	// Filter binaries from selection unless -b.
	if !*allowBinary {
		dst := files[:0]
		for _, relSlash := range files {
			abs := filepath.Join(base, filepath.FromSlash(relSlash))
			if isBinary(abs) {
				continue
			}
			dst = append(dst, relSlash)
		}
		files = dst
	}

	root := buildTree(startRelSlash, files)

	m := newModel(root, base, inRepo, *allowBinary)
	p := tea.NewProgram(m, tea.WithAltScreen())

	final, err := p.Run()
	if err != nil {
		panic(err)
	}

	fm := final.(model)
	if fm.aborted {
		return
	}

	if fm.confirmed {
		selected := fm.selectedFiles()
		outAbs, size, tokens := buildMarkdown(fm.base, selected, fm.allowBinary)
		fmt.Printf("path=%s\nbytes=%d\ntokens=%d\n", outAbs, size, tokens)
	}
}
