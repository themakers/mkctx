# mkctx

Minimalist TUI tool to interactively assemble **one Markdown document** from selected files in a git repository or the current directory.  
Designed to produce clean, deterministic context files for LLMs.

Built with **Go + Bubble Tea**. Focus: simplicity, readability, zero over-engineering.

---

## What it does

- Shows a **file tree TUI** starting from the current directory
- Respects **gitignore exactly like Git** (all `.gitignore`, proper scope & priority)
- Lets you **select files interactively**
- Builds a single Markdown file with:
  - relative paths
  - fenced code blocks
  - automatic language detection
  - **collision-safe code fences** (no ``` breakage)
- Writes output to `.mkctx/` in the repo root (or cwd if no repo)

---

## Usage

```bash
mkctx        # text files only
mkctx -b     # allow binary files (uses `file <path>` output)
````

### Key bindings

| Key     | Action                 |
| ------- | ---------------------- |
| ↑ / ↓   | Move cursor            |
| →       | Expand directory       |
| ←       | Collapse directory     |
| Space   | Select / unselect file |
| Enter   | Build markdown         |
| q / Esc | Quit without building  |

Only files can be selected (not directories).

---

## Git behavior

* On start, walks up to find `.git/`
* If found:

  * uses repo root as base path
  * file list is obtained via `git ls-files --exclude-standard`
* If not found:

  * works in current directory
  * no ignore rules applied
* `.git/` is always hidden

---

## Output

* Directory: `.mkctx/`
* Filename:

  ```
  source-context-YYYY-MM-DD-HH-MM-SS.md
  ```
* Markdown format per file:

  ````markdown
  ## path/to/file.ext

  ```lang
  file contents
  ````

  ```
  ```
* After success, prints to `stdout`:

  ```
  path=/abs/path/to/file.md
  bytes=12345
  tokens=3086
  ```

(Token estimate ≈ bytes / 4)

---

## Design principles

* Panic on unexpected errors (no graceful degradation)
* Deterministic output
* Sequential I/O (no full-document buffering)
* Handles repositories with thousands of files
* Minimal code, no abstractions “for the future”

---

## Build

```bash
make build
```

Requires Go ≥ 1.25 and `git` in PATH.

```
```
